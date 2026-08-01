package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

const (
	ContentModerationActionAllow        = "allow"
	ContentModerationActionBlock        = "block"
	ContentModerationActionHashBlock    = "hash_block"
	ContentModerationActionKeywordBlock = "keyword_block"
	ContentModerationActionCyberPolicy  = "cyber_policy"
	ContentModerationActionError        = "error"

	contentModerationConfigChannel = "newapi:content_moderation:config:invalidate"
	contentModerationCleanupDelay  = 5 * time.Minute
	contentModerationCleanupPeriod = 24 * time.Hour
	contentModerationMaxExcerpt    = 240
)

type ContentModerationCheckInput struct {
	RequestID string
	UserID    int
	Username  string
	UserEmail string
	TokenID   int
	TokenName string
	Group     string
	Endpoint  string
	Provider  string
	Model     string
	Protocol  string
	Body      []byte
}

type ContentModerationDecision struct {
	Allowed         bool               `json:"allowed"`
	Blocked         bool               `json:"blocked"`
	Flagged         bool               `json:"flagged"`
	Message         string             `json:"message"`
	StatusCode      int                `json:"status_code"`
	InputHash       string             `json:"input_hash,omitempty"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CompositeScore  float64            `json:"composite_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Thresholds      map[string]float64 `json:"thresholds"`
	Action          string             `json:"action"`
}

type ContentModerationAPIKeyStatus struct {
	Index          int        `json:"index"`
	KeyHash        string     `json:"key_hash"`
	Masked         string     `json:"masked"`
	Status         string     `json:"status"`
	FailureCount   int        `json:"failure_count"`
	SuccessCount   int64      `json:"success_count"`
	LastError      string     `json:"last_error"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	FrozenUntil    *time.Time `json:"frozen_until,omitempty"`
	LastLatencyMS  int        `json:"last_latency_ms"`
	LastHTTPStatus int        `json:"last_http_status"`
	Active         int64      `json:"active"`
	TotalCalls     int64      `json:"total_calls"`
	ErrorCount     int64      `json:"error_count"`
	AverageLatency int64      `json:"average_latency_ms"`
	Configured     bool       `json:"configured"`
}

type ContentModerationRuntimeStatus struct {
	Enabled              bool                            `json:"enabled"`
	Mode                 string                          `json:"mode"`
	WorkerCount          int                             `json:"worker_count"`
	MaxWorkers           int                             `json:"max_workers"`
	ActiveWorkers        int64                           `json:"active_workers"`
	QueueSize            int                             `json:"queue_size"`
	QueueLength          int                             `json:"queue_length"`
	QueueUsagePercent    float64                         `json:"queue_usage_percent"`
	Enqueued             int64                           `json:"enqueued"`
	Dropped              int64                           `json:"dropped"`
	Processed            int64                           `json:"processed"`
	Errors               int64                           `json:"errors"`
	PreBlockActive       int64                           `json:"pre_block_active"`
	PreBlockChecked      int64                           `json:"pre_block_checked"`
	PreBlockAllowed      int64                           `json:"pre_block_allowed"`
	PreBlockBlocked      int64                           `json:"pre_block_blocked"`
	PreBlockErrors       int64                           `json:"pre_block_errors"`
	PreBlockAvgLatency   int64                           `json:"pre_block_avg_latency_ms"`
	PreBlockKeyActive    int64                           `json:"pre_block_api_key_active"`
	PreBlockKeyAvailable int64                           `json:"pre_block_api_key_available_count"`
	PreBlockKeyTotal     int64                           `json:"pre_block_api_key_total_calls"`
	APIKeyStatuses       []ContentModerationAPIKeyStatus `json:"api_key_statuses"`
	FlaggedHashCount     int64                           `json:"flagged_hash_count"`
	LastCleanupAt        *time.Time                      `json:"last_cleanup_at,omitempty"`
	LastCleanupHit       int64                           `json:"last_cleanup_deleted_hit"`
	LastCleanupNonHit    int64                           `json:"last_cleanup_deleted_non_hit"`
	ConfigVersion        int64                           `json:"config_version"`
	PromptAuditActive    bool                            `json:"prompt_audit_active"`
	EncryptionAvailable  bool                            `json:"encryption_key_configured"`
}

type ContentModerationTestResult struct {
	Items       []ContentModerationAPIKeyStatus `json:"items"`
	AuditResult *ContentModerationDecision      `json:"audit_result,omitempty"`
	ImageCount  int                             `json:"image_count"`
}

type contentModerationTask struct {
	input       ContentModerationCheckInput
	content     ContentModerationInput
	inputHash   string
	config      setting.ContentModerationStorageConfig
	log         *model.ContentModerationLog
	recordHash  bool
	sideEffects bool
	enqueuedAt  time.Time
}

type contentModerationKeyHealth struct {
	failureCount   int
	successCount   int64
	lastError      string
	lastCheckedAt  time.Time
	frozenUntil    time.Time
	lastLatencyMS  int
	lastHTTPStatus int
	active         int64
	totalCalls     int64
	errorCount     int64
	totalLatencyMS int64
}

type contentModerationRuntimeState struct {
	once              sync.Once
	started           atomic.Bool
	cancel            context.CancelFunc
	waitGroup         sync.WaitGroup
	queue             chan contentModerationTask
	cursor            atomic.Uint64
	active            atomic.Int64
	enqueued          atomic.Int64
	dropped           atomic.Int64
	processed         atomic.Int64
	errors            atomic.Int64
	preActive         atomic.Int64
	preChecked        atomic.Int64
	preAllowed        atomic.Int64
	preBlocked        atomic.Int64
	preErrors         atomic.Int64
	preLatency        atomic.Int64
	lastCleanup       atomic.Int64
	lastCleanupHit    atomic.Int64
	lastCleanupNonHit atomic.Int64
	healthMu          sync.Mutex
	health            map[string]*contentModerationKeyHealth
}

var contentModerationRuntime = contentModerationRuntimeState{
	queue:  make(chan contentModerationTask, setting.MaxContentModerationQueueSize),
	health: make(map[string]*contentModerationKeyHealth),
}

func StartContentModerationService() {
	contentModerationRuntime.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		contentModerationRuntime.cancel = cancel
		contentModerationRuntime.started.Store(true)
		for workerID := 0; workerID < setting.MaxContentModerationWorkers; workerID++ {
			contentModerationRuntime.waitGroup.Add(1)
			go func(id int) {
				defer contentModerationRuntime.waitGroup.Done()
				contentModerationWorker(ctx, id)
			}(workerID)
		}
		contentModerationRuntime.waitGroup.Add(1)
		go func() {
			defer contentModerationRuntime.waitGroup.Done()
			contentModerationCleanupWorker(ctx)
		}()
		if common.RedisEnabled && common.RDB != nil {
			contentModerationRuntime.waitGroup.Add(1)
			go func() {
				defer contentModerationRuntime.waitGroup.Done()
				contentModerationConfigSubscriber(ctx)
			}()
		}
	})
}

func StopContentModerationService(ctx context.Context) error {
	if !contentModerationRuntime.started.Load() {
		return nil
	}
	if contentModerationRuntime.cancel != nil {
		contentModerationRuntime.cancel()
	}
	done := make(chan struct{})
	go func() {
		contentModerationRuntime.waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
		contentModerationRuntime.started.Store(false)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func CheckContentModeration(ctx context.Context, input ContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	config, err := setting.GetContentModerationStorageConfig()
	if err != nil || !setting.ContentModerationStorageActive(config) || !contentModerationIncludesGroup(config, input.Group) || !contentModerationIncludesModel(config, input.Model) {
		return allow, err
	}
	content := ExtractContentModerationInput(input.Protocol, input.Body)
	if content.Empty() {
		return allow, nil
	}
	inputHash := content.Hash()

	if config.Mode == setting.ContentModerationModePreBlock && config.KeywordBlockingMode != setting.ContentModerationAPIOnly {
		if keyword := matchContentModerationKeyword(content.Text, config.BlockedKeywords); keyword != "" {
			scores := map[string]float64{"keyword": 1}
			log := buildContentModerationLog(input, config, ContentModerationActionKeywordBlock, true, "keyword", 1, keyword, scores, content.Text, nil, nil, "")
			enqueueContentModerationRecord(config, input, log, inputHash, false, true)
			recordContentModerationPreBlock(0, ContentModerationActionKeywordBlock)
			return blockedContentModerationDecision(config, ContentModerationActionKeywordBlock, inputHash, "keyword", 1, scores), nil
		}
		if config.KeywordBlockingMode == setting.ContentModerationKeywordOnly {
			recordContentModerationPreBlock(0, ContentModerationActionAllow)
			return allow, nil
		}
	}

	if config.Mode == setting.ContentModerationModePreBlock && config.PreHashCheckEnabled {
		matched, hashErr := model.HasContentModerationHash(ctx, inputHash)
		if hashErr != nil {
			common.SysError("content moderation hash lookup failed: " + hashErr.Error())
		}
		if matched {
			scores := map[string]float64{"hash": 1}
			log := buildContentModerationLog(input, config, ContentModerationActionHashBlock, true, "hash", 1, "", scores, content.Text, nil, nil, "")
			enqueueContentModerationRecord(config, input, log, inputHash, false, false)
			recordContentModerationPreBlock(0, ContentModerationActionHashBlock)
			return blockedContentModerationDecision(config, ContentModerationActionHashBlock, inputHash, "hash", 1, scores), nil
		}
	}

	if !contentModerationShouldSample(config.SampleRate, inputHash) {
		if config.Mode == setting.ContentModerationModePreBlock {
			recordContentModerationPreBlock(0, ContentModerationActionAllow)
		}
		return allow, nil
	}
	keys, _ := setting.DecryptContentModerationAPIKeys(config)
	if len(keys) == 0 {
		if config.Mode == setting.ContentModerationModePreBlock {
			recordContentModerationPreBlock(0, ContentModerationActionError)
		}
		return allow, nil
	}
	if config.Mode == setting.ContentModerationModeObserve {
		enqueueContentModerationAudit(config, input, content, inputHash)
		return allow, nil
	}
	return runContentModerationCheck(ctx, config, input, content, inputHash, nil, true), nil
}

func runContentModerationCheck(ctx context.Context, config setting.ContentModerationStorageConfig, input ContentModerationCheckInput, content ContentModerationInput, inputHash string, queueDelay *int, allowBlock bool) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	trackPreBlock := allowBlock && queueDelay == nil && config.Mode == setting.ContentModerationModePreBlock
	if trackPreBlock {
		contentModerationRuntime.preActive.Add(1)
		defer contentModerationRuntime.preActive.Add(-1)
	}
	started := time.Now()
	result, err := callContentModeration(ctx, config, content.APIInput())
	latency := int(time.Since(started).Milliseconds())
	if err != nil {
		if queueDelay != nil {
			contentModerationRuntime.errors.Add(1)
		}
		if trackPreBlock {
			recordContentModerationPreBlock(latency, ContentModerationActionError)
		}
		if config.RecordNonHits {
			log := buildContentModerationLog(input, config, ContentModerationActionError, false, "", 0, "", nil, content.Text, &latency, queueDelay, err.Error())
			log.InputHash = inputHash
			_ = model.CreateContentModerationLog(context.Background(), log)
		}
		return allow
	}
	flagged, category, score := evaluateContentModerationScores(result.CategoryScores, config.Thresholds)
	action := ContentModerationActionAllow
	blocked := flagged && allowBlock && config.Mode == setting.ContentModerationModePreBlock
	if blocked {
		action = ContentModerationActionBlock
	}
	if trackPreBlock {
		recordContentModerationPreBlock(latency, action)
	}
	if flagged || config.RecordNonHits {
		log := buildContentModerationLog(input, config, action, flagged, category, score, "", result.CategoryScores, content.Text, &latency, queueDelay, "")
		if queueDelay == nil && config.Mode == setting.ContentModerationModePreBlock {
			enqueueContentModerationRecord(config, input, log, inputHash, flagged, flagged)
		} else {
			persistContentModerationLog(context.Background(), config, log, inputHash, flagged, flagged)
		}
	}
	if blocked {
		return blockedContentModerationDecision(config, action, inputHash, category, score, result.CategoryScores)
	}
	return &ContentModerationDecision{Allowed: true, Flagged: flagged, HighestCategory: category, HighestScore: score, CategoryScores: result.CategoryScores, Action: action}
}

func blockedContentModerationDecision(config setting.ContentModerationStorageConfig, action, inputHash, category string, score float64, scores map[string]float64) *ContentModerationDecision {
	return &ContentModerationDecision{
		Allowed: false, Blocked: true, Flagged: true, Message: config.BlockMessage,
		StatusCode: config.BlockStatus, InputHash: inputHash, HighestCategory: category,
		HighestScore: score, CategoryScores: scores, Action: action,
	}
}

func enqueueContentModerationAudit(config setting.ContentModerationStorageConfig, input ContentModerationCheckInput, content ContentModerationInput, inputHash string) {
	task := contentModerationTask{input: input, content: content, inputHash: inputHash, enqueuedAt: time.Now()}
	enqueueContentModerationTask(config.QueueSize, task)
}

func enqueueContentModerationRecord(config setting.ContentModerationStorageConfig, input ContentModerationCheckInput, log *model.ContentModerationLog, inputHash string, recordHash, sideEffects bool) {
	task := contentModerationTask{input: input, config: config, log: log, inputHash: inputHash, recordHash: recordHash, sideEffects: sideEffects, enqueuedAt: time.Now()}
	enqueueContentModerationTask(config.QueueSize, task)
}

func enqueueContentModerationTask(queueSize int, task contentModerationTask) {
	if queueSize <= 0 || queueSize > setting.MaxContentModerationQueueSize {
		queueSize = setting.DefaultContentModerationQueueSize
	}
	if len(contentModerationRuntime.queue) >= queueSize {
		contentModerationRuntime.dropped.Add(1)
		return
	}
	select {
	case contentModerationRuntime.queue <- task:
		contentModerationRuntime.enqueued.Add(1)
	default:
		contentModerationRuntime.dropped.Add(1)
	}
}

func contentModerationWorker(ctx context.Context, workerID int) {
	for {
		config, err := setting.GetContentModerationStorageConfig()
		if err != nil || workerID >= config.WorkerCount {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case task := <-contentModerationRuntime.queue:
			contentModerationRuntime.active.Add(1)
			func() {
				defer contentModerationRuntime.active.Add(-1)
				defer contentModerationRuntime.processed.Add(1)
				defer func() {
					if recovered := recover(); recovered != nil {
						contentModerationRuntime.errors.Add(1)
						common.SysError(fmt.Sprintf("content moderation worker panic: %v", recovered))
					}
				}()
				queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
				if task.log != nil {
					task.log.QueueDelayMS = &queueDelay
					persistContentModerationLog(context.Background(), task.config, task.log, task.inputHash, task.recordHash, task.sideEffects)
					return
				}
				current, loadErr := setting.GetContentModerationStorageConfig()
				if loadErr != nil || !setting.ContentModerationStorageActive(current) || current.Mode != setting.ContentModerationModeObserve || !contentModerationIncludesGroup(current, task.input.Group) || !contentModerationIncludesModel(current, task.input.Model) {
					return
				}
				timeout := time.Duration(current.TimeoutMS*(current.RetryCount+1)+5000) * time.Millisecond
				workerCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				_ = runContentModerationCheck(workerCtx, current, task.input, task.content, task.inputHash, &queueDelay, false)
			}()
		}
	}
}

func recordContentModerationPreBlock(latency int, action string) {
	contentModerationRuntime.preChecked.Add(1)
	if latency > 0 {
		contentModerationRuntime.preLatency.Add(int64(latency))
	}
	switch action {
	case ContentModerationActionBlock, ContentModerationActionHashBlock, ContentModerationActionKeywordBlock:
		contentModerationRuntime.preBlocked.Add(1)
	case ContentModerationActionError:
		contentModerationRuntime.preErrors.Add(1)
	default:
		contentModerationRuntime.preAllowed.Add(1)
	}
}

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type contentModerationAPIInputPart struct {
	Type     string                     `json:"type"`
	Text     string                     `json:"text,omitempty"`
	ImageURL *contentModerationImageURL `json:"image_url,omitempty"`
}

type contentModerationImageURL struct {
	URL string `json:"url"`
}

type moderationAPIResponse struct {
	Results []moderationAPIResult `json:"results"`
}

type moderationAPIResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

type moderationHTTPError struct {
	status int
	text   string
}

func (err *moderationHTTPError) Error() string { return err.text }

func callContentModeration(ctx context.Context, config setting.ContentModerationStorageConfig, input any) (*moderationAPIResult, error) {
	keys, _ := setting.DecryptContentModerationAPIKeys(config)
	attempts := config.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		key, ok := nextContentModerationAPIKey(keys)
		if !ok {
			return nil, errors.New("no usable content moderation API key")
		}
		started := time.Now()
		beginContentModerationAPIKeyCall(key)
		result, err := callContentModerationOnce(ctx, config, key, input)
		latency := int(time.Since(started).Milliseconds())
		if err == nil {
			markContentModerationAPIKey(key, nil, latency, http.StatusOK)
			return result, nil
		}
		lastErr = err
		status := 0
		var httpErr *moderationHTTPError
		if errors.As(err, &httpErr) {
			status = httpErr.status
		}
		markContentModerationAPIKey(key, err, latency, status)
		if status == http.StatusBadRequest || attempt+1 >= attempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(100*(attempt+1)) * time.Millisecond):
		}
	}
	return nil, lastErr
}

func callContentModerationOnce(ctx context.Context, config setting.ContentModerationStorageConfig, apiKey string, input any) (*moderationAPIResult, error) {
	endpoint, err := contentModerationEndpoint(config.BaseURL)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(moderationAPIRequest{Model: config.Model, Input: input})
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutMS)*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	client, err := GetHttpClientWithProxy(config.ProxyURL)
	if err != nil {
		return nil, err
	}
	requestClient := *client
	requestClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := requestClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, &moderationHTTPError{status: response.StatusCode, text: fmt.Sprintf("moderation API status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))}
	}
	var output moderationAPIResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&output); err != nil {
		return nil, err
	}
	if len(output.Results) == 0 {
		return nil, errors.New("moderation API returned no result")
	}
	return &output.Results[0], nil
}

func contentModerationEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/v1/moderations") {
		parsed.Path = path
	} else if strings.HasSuffix(path, "/v1") {
		parsed.Path = path + "/moderations"
	} else {
		parsed.Path = path + "/v1/moderations"
	}
	return parsed.String(), nil
}

func nextContentModerationAPIKey(keys []string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	now := time.Now()
	for index := 0; index < len(keys); index++ {
		cursor := int(contentModerationRuntime.cursor.Add(1)-1) % len(keys)
		key := keys[cursor]
		hash := contentModerationAPIKeyHash(key)
		contentModerationRuntime.healthMu.Lock()
		health := contentModerationRuntime.health[hash]
		frozen := health != nil && health.frozenUntil.After(now)
		contentModerationRuntime.healthMu.Unlock()
		if !frozen {
			return key, true
		}
	}
	return "", false
}

func markContentModerationAPIKey(key string, callErr error, latency, status int) {
	hash := contentModerationAPIKeyHash(key)
	contentModerationRuntime.healthMu.Lock()
	defer contentModerationRuntime.healthMu.Unlock()
	health := contentModerationRuntime.health[hash]
	if health == nil {
		health = &contentModerationKeyHealth{}
		contentModerationRuntime.health[hash] = health
	}
	health.lastCheckedAt = time.Now()
	health.lastLatencyMS = latency
	health.lastHTTPStatus = status
	if health.active > 0 {
		health.active--
	}
	health.totalCalls++
	health.totalLatencyMS += int64(latency)
	if callErr == nil {
		health.successCount++
		health.failureCount = 0
		health.lastError = ""
		health.frozenUntil = time.Time{}
		return
	}
	health.failureCount++
	health.errorCount++
	health.lastError = callErr.Error()
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		health.frozenUntil = time.Now().Add(10 * time.Minute)
	case http.StatusTooManyRequests:
		health.frozenUntil = time.Now().Add(time.Minute)
	default:
		health.frozenUntil = time.Now().Add(10 * time.Second)
	}
}

func beginContentModerationAPIKeyCall(key string) {
	hash := contentModerationAPIKeyHash(key)
	contentModerationRuntime.healthMu.Lock()
	health := contentModerationRuntime.health[hash]
	if health == nil {
		health = &contentModerationKeyHealth{}
		contentModerationRuntime.health[hash] = health
	}
	health.active++
	contentModerationRuntime.healthMu.Unlock()
}

func ContentModerationAPIKeyStatuses(config setting.ContentModerationStorageConfig) []ContentModerationAPIKeyStatus {
	result := make([]ContentModerationAPIKeyStatus, 0, len(config.APIKeyCiphertexts))
	for index, ciphertext := range config.APIKeyCiphertexts {
		key, err := setting.DecryptPromptAuditToken(ciphertext)
		if err != nil || strings.TrimSpace(key) == "" {
			hash := contentModerationAPIKeyHash(ciphertext)
			result = append(result, ContentModerationAPIKeyStatus{Index: index, KeyHash: hash, Masked: "invalid", Status: "invalid", Configured: true})
			continue
		}
		result = append(result, contentModerationAPIKeyStatus(index, contentModerationAPIKeyHash(key), maskContentModerationAPIKey(key), true))
	}
	return result
}

func contentModerationAPIKeyStatus(index int, hash, masked string, configured bool) ContentModerationAPIKeyStatus {
	status := ContentModerationAPIKeyStatus{Index: index, KeyHash: hash, Masked: masked, Status: "ready", Configured: configured}
	contentModerationRuntime.healthMu.Lock()
	health := contentModerationRuntime.health[hash]
	if health != nil {
		status.FailureCount = health.failureCount
		status.SuccessCount = health.successCount
		status.LastError = health.lastError
		status.LastLatencyMS = health.lastLatencyMS
		status.LastHTTPStatus = health.lastHTTPStatus
		status.Active = health.active
		status.TotalCalls = health.totalCalls
		status.ErrorCount = health.errorCount
		if health.totalCalls > 0 {
			status.AverageLatency = health.totalLatencyMS / health.totalCalls
		}
		if !health.lastCheckedAt.IsZero() {
			value := health.lastCheckedAt
			status.LastCheckedAt = &value
		}
		if health.frozenUntil.After(time.Now()) {
			value := health.frozenUntil
			status.FrozenUntil = &value
			status.Status = "frozen"
		} else if health.lastError != "" {
			status.Status = "degraded"
		}
	}
	contentModerationRuntime.healthMu.Unlock()
	return status
}

func TestContentModerationAPIKeys(ctx context.Context, config setting.ContentModerationStorageConfig, keys []string, prompt string, images []string) (ContentModerationTestResult, error) {
	if err := validateContentModerationTestImages(images); err != nil {
		return ContentModerationTestResult{}, err
	}
	configured := len(keys) == 0
	if configured {
		keys, _ = setting.DecryptContentModerationAPIKeys(config)
	}
	keys = normalizeContentModerationAPIKeys(keys)
	input := ContentModerationInput{Text: prompt, Images: images}
	input.Normalize()
	if input.Empty() {
		input.Text = "This is a benign moderation API connectivity test."
	}
	result := ContentModerationTestResult{Items: make([]ContentModerationAPIKeyStatus, 0, len(keys)), ImageCount: len(input.Images)}
	for index, key := range keys {
		started := time.Now()
		beginContentModerationAPIKeyCall(key)
		apiResult, err := callContentModerationOnce(ctx, config, key, input.APIInput())
		latency := int(time.Since(started).Milliseconds())
		statusCode := http.StatusOK
		if err != nil {
			var httpErr *moderationHTTPError
			if errors.As(err, &httpErr) {
				statusCode = httpErr.status
			} else {
				statusCode = 0
			}
		}
		markContentModerationAPIKey(key, err, latency, statusCode)
		item := contentModerationAPIKeyStatus(index, contentModerationAPIKeyHash(key), maskContentModerationAPIKey(key), configured)
		result.Items = append(result.Items, item)
		if err == nil && result.AuditResult == nil {
			flagged, category, score := evaluateContentModerationScores(apiResult.CategoryScores, config.Thresholds)
			result.AuditResult = &ContentModerationDecision{Allowed: !flagged, Flagged: flagged, HighestCategory: category, HighestScore: score, CompositeScore: score, CategoryScores: apiResult.CategoryScores, Thresholds: config.Thresholds, Action: ContentModerationActionAllow}
		}
	}
	return result, nil
}

func validateContentModerationTestImages(images []string) error {
	if len(images) > maxContentModerationImages {
		return fmt.Errorf("content moderation test accepts at most %d image", maxContentModerationImages)
	}
	for _, image := range images {
		image = strings.TrimSpace(image)
		if !strings.HasPrefix(image, "data:image/") {
			continue
		}
		comma := strings.IndexByte(image, ',')
		if comma < 0 || !strings.Contains(strings.ToLower(image[:comma]), ";base64") {
			return errors.New("content moderation test image must be a base64 data URL")
		}
		decodedLength := base64.StdEncoding.DecodedLen(len(image) - comma - 1)
		if decodedLength > 8*1024*1024 {
			return errors.New("content moderation test image must not exceed 8 MB")
		}
		if _, err := base64.StdEncoding.DecodeString(image[comma+1:]); err != nil {
			return errors.New("content moderation test image contains invalid base64 data")
		}
	}
	return nil
}

func GetContentModerationRuntimeStatus() ContentModerationRuntimeStatus {
	config, _ := setting.GetContentModerationStorageConfig()
	checked := contentModerationRuntime.preChecked.Load()
	average := int64(0)
	if checked > 0 {
		average = contentModerationRuntime.preLatency.Load() / checked
	}
	hashes, _ := model.CountContentModerationHashes(context.Background())
	statuses := ContentModerationAPIKeyStatuses(config)
	var keyActive, keyAvailable, keyTotal int64
	for _, status := range statuses {
		keyActive += status.Active
		keyTotal += status.TotalCalls
		if status.Status == "ready" || status.Status == "degraded" {
			keyAvailable++
		}
	}
	queueLength := len(contentModerationRuntime.queue)
	usage := float64(0)
	if config.QueueSize > 0 {
		usage = float64(queueLength) * 100 / float64(config.QueueSize)
	}
	var cleanupAt *time.Time
	if value := contentModerationRuntime.lastCleanup.Load(); value > 0 {
		parsed := time.Unix(value, 0)
		cleanupAt = &parsed
	}
	return ContentModerationRuntimeStatus{
		Enabled: config.Enabled, Mode: config.Mode, WorkerCount: config.WorkerCount,
		MaxWorkers: setting.MaxContentModerationWorkers, ActiveWorkers: contentModerationRuntime.active.Load(),
		QueueSize: config.QueueSize, QueueLength: queueLength, QueueUsagePercent: usage,
		Enqueued: contentModerationRuntime.enqueued.Load(), Dropped: contentModerationRuntime.dropped.Load(),
		Processed: contentModerationRuntime.processed.Load(), Errors: contentModerationRuntime.errors.Load(),
		PreBlockActive: contentModerationRuntime.preActive.Load(), PreBlockChecked: checked,
		PreBlockAllowed: contentModerationRuntime.preAllowed.Load(), PreBlockBlocked: contentModerationRuntime.preBlocked.Load(),
		PreBlockErrors: contentModerationRuntime.preErrors.Load(), PreBlockAvgLatency: average,
		PreBlockKeyActive: keyActive, PreBlockKeyAvailable: keyAvailable, PreBlockKeyTotal: keyTotal,
		APIKeyStatuses: statuses, FlaggedHashCount: hashes,
		LastCleanupAt: cleanupAt, LastCleanupHit: contentModerationRuntime.lastCleanupHit.Load(),
		LastCleanupNonHit: contentModerationRuntime.lastCleanupNonHit.Load(), ConfigVersion: config.ConfigVersion,
		PromptAuditActive:   setting.PromptAuditJSONActive(setting.PromptAuditConfigJSON),
		EncryptionAvailable: setting.PromptAuditEncryptionKeyConfigured(),
	}
}

func contentModerationCleanupWorker(ctx context.Context) {
	timer := time.NewTimer(contentModerationCleanupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			config, err := setting.GetContentModerationStorageConfig()
			if err == nil {
				now := time.Now()
				hit, nonHit, cleanupErr := model.CleanupContentModerationLogs(ctx, now.Add(-time.Duration(config.HitRetentionDays)*24*time.Hour).Unix(), now.Add(-time.Duration(config.NonHitRetentionDays)*24*time.Hour).Unix())
				if cleanupErr == nil {
					contentModerationRuntime.lastCleanup.Store(now.Unix())
					contentModerationRuntime.lastCleanupHit.Store(hit)
					contentModerationRuntime.lastCleanupNonHit.Store(nonHit)
				}
			}
			timer.Reset(contentModerationCleanupPeriod)
		}
	}
}

func contentModerationConfigSubscriber(ctx context.Context) {
	pubsub := common.RDB.Subscribe(ctx, contentModerationConfigChannel)
	defer pubsub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-pubsub.Channel():
			if !ok {
				return
			}
			if err := model.ReloadOption(setting.ContentModerationConfigOptionKey); err != nil {
				common.SysError("content moderation config reload failed: " + err.Error())
			}
		}
	}
}

func PublishContentModerationConfigInvalidation(ctx context.Context, version int64) {
	if common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Publish(ctx, contentModerationConfigChannel, strconv.FormatInt(version, 10)).Err()
	}
}

func buildContentModerationLog(input ContentModerationCheckInput, config setting.ContentModerationStorageConfig, action string, flagged bool, category string, score float64, keyword string, scores map[string]float64, excerpt string, latency, queueDelay *int, errorText string) *model.ContentModerationLog {
	scoresJSON, _ := json.Marshal(scores)
	thresholdsJSON, _ := json.Marshal(config.Thresholds)
	return &model.ContentModerationLog{
		RequestId: input.RequestID, UserId: input.UserID, Username: input.Username, UserEmail: input.UserEmail,
		TokenId: input.TokenID, TokenName: input.TokenName, GroupName: input.Group, Endpoint: input.Endpoint,
		Provider: input.Provider, Protocol: input.Protocol, Model: input.Model, Mode: config.Mode, Action: action,
		Flagged: flagged, HighestCategory: category, HighestScore: score, MatchedKeyword: keyword,
		CategoryScoresJSON: string(scoresJSON), ThresholdSnapshotJSON: string(thresholdsJSON),
		InputExcerpt: redactContentModerationExcerpt(excerpt), UpstreamLatencyMS: latency, QueueDelayMS: queueDelay, Error: contentModerationTrimRunes(errorText, 1000),
	}
}

func persistContentModerationLog(ctx context.Context, config setting.ContentModerationStorageConfig, log *model.ContentModerationLog, inputHash string, recordHash, sideEffects bool) {
	if log == nil {
		return
	}
	log.InputHash = inputHash
	if recordHash {
		_ = model.RecordContentModerationHash(ctx, inputHash)
	}
	if sideEffects && log.Flagged && log.UserId > 0 {
		since := time.Now().Add(-time.Duration(config.ViolationWindowHours) * time.Hour).Unix()
		count, err := model.CountContentModerationViolations(ctx, log.UserId, since, config.CyberPolicyExcludeFromBanCount)
		if err == nil {
			log.ViolationCount = int(count) + 1
		}
		if config.AutoBanEnabled && log.ViolationCount >= config.BanThreshold {
			if err := model.SetUserStatusForContentModeration(log.UserId, common.UserStatusDisabled); err == nil {
				log.AutoBanned = true
			}
		}
		if config.EmailOnHit && strings.TrimSpace(log.UserEmail) != "" {
			subject := fmt.Sprintf("[%s] 内容审核风险提醒", common.SystemName)
			body := fmt.Sprintf("<p>您的请求触发了内容审核规则。</p><p>类别：%s<br>分数：%.4f<br>请求时间：%s</p>", html.EscapeString(log.HighestCategory), log.HighestScore, time.Now().Format(time.RFC3339))
			if err := common.SendEmail(subject, log.UserEmail, body); err == nil {
				log.EmailSent = true
			}
		}
	}
	if err := model.CreateContentModerationLog(ctx, log); err != nil {
		contentModerationRuntime.errors.Add(1)
		common.SysError("content moderation log write failed: " + err.Error())
	}
}

func DecodeContentModerationLog(log model.ContentModerationLog) map[string]any {
	raw, _ := json.Marshal(log)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	scores := map[string]float64{}
	thresholds := map[string]float64{}
	_ = json.Unmarshal([]byte(log.CategoryScoresJSON), &scores)
	_ = json.Unmarshal([]byte(log.ThresholdSnapshotJSON), &thresholds)
	result["category_scores"] = scores
	result["threshold_snapshot"] = thresholds
	return result
}

func contentModerationIncludesGroup(config setting.ContentModerationStorageConfig, group string) bool {
	if config.AllGroups {
		return true
	}
	for _, configured := range config.Groups {
		if strings.EqualFold(strings.TrimSpace(configured), strings.TrimSpace(group)) {
			return true
		}
	}
	return false
}

func contentModerationIncludesModel(config setting.ContentModerationStorageConfig, modelName string) bool {
	matched := false
	for _, configured := range config.ModelFilter.Models {
		if strings.EqualFold(strings.TrimSpace(configured), strings.TrimSpace(modelName)) {
			matched = true
			break
		}
	}
	switch config.ModelFilter.Type {
	case setting.ContentModerationModelInclude:
		return matched
	case setting.ContentModerationModelExclude:
		return !matched
	default:
		return true
	}
}

func contentModerationShouldSample(rate int, inputHash string) bool {
	if rate >= 100 {
		return true
	}
	if rate <= 0 {
		return false
	}
	raw, err := hex.DecodeString(inputHash)
	if err != nil || len(raw) < 2 {
		return true
	}
	return int(binary.BigEndian.Uint16(raw[:2])%100) < rate
}

func matchContentModerationKeyword(text string, keywords []string) string {
	lower := strings.ToLower(text)
	for _, keyword := range keywords {
		if value := strings.TrimSpace(keyword); value != "" && strings.Contains(lower, strings.ToLower(value)) {
			return value
		}
	}
	return ""
}

func evaluateContentModerationScores(scores, thresholds map[string]float64) (bool, string, float64) {
	flagged := false
	highestCategory := ""
	highestScore := float64(0)
	for _, category := range setting.ContentModerationCategoryOrder {
		score := scores[category]
		if score > highestScore || highestCategory == "" {
			highestCategory, highestScore = category, score
		}
		if threshold, ok := thresholds[category]; ok && score >= threshold {
			flagged = true
		}
	}
	for category, score := range scores {
		if score > highestScore || highestCategory == "" {
			highestCategory, highestScore = category, score
		}
	}
	return flagged, highestCategory, highestScore
}

func normalizeContentModerationAPIKeys(keys []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		hash := contentModerationAPIKeyHash(key)
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		result = append(result, key)
	}
	return result
}

func contentModerationAPIKeyHash(key string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(hash[:])
}

func ContentModerationAPIKeyIdentifier(key string) string {
	return contentModerationAPIKeyHash(key)
}

func maskContentModerationAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

func redactContentModerationExcerpt(value string) string {
	return redactPromptAuditValue(value, contentModerationMaxExcerpt)
}
