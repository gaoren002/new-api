package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

const (
	promptAuditPayloadTTL     = 30 * time.Minute
	promptAuditJobLease       = 3 * time.Minute
	promptAuditWorkerPoll     = 500 * time.Millisecond
	promptAuditLatencySamples = 2048
	promptAuditEnqueueSlots   = 128
)

type PromptAuditProbeResult struct {
	OK           bool      `json:"ok"`
	Status       string    `json:"status"`
	ErrorCode    string    `json:"error_code,omitempty"`
	Message      string    `json:"message"`
	LatencyMS    int64     `json:"latency_ms"`
	HTTPStatus   int       `json:"http_status"`
	Retryable    bool      `json:"retryable"`
	CheckedAt    time.Time `json:"checked_at"`
	TokenApplied bool      `json:"token_applied"`
}

type PromptAuditMetricsSnapshot struct {
	Total        int64 `json:"total"`
	Allowed      int64 `json:"allowed"`
	Flagged      int64 `json:"flagged"`
	Blocked      int64 `json:"blocked"`
	Unavailable  int64 `json:"unavailable"`
	Invalid      int64 `json:"invalid"`
	Timeouts     int64 `json:"timeouts"`
	Failovers    int64 `json:"failovers"`
	BulkheadFull int64 `json:"bulkhead_full"`
	RecordFailed int64 `json:"record_failed"`
	LatencyCount int64 `json:"latency_count"`
	LatencyAvgMS int64 `json:"latency_avg_ms"`
	LatencyP50MS int64 `json:"latency_p50_ms"`
	LatencyP95MS int64 `json:"latency_p95_ms"`
	LatencyP99MS int64 `json:"latency_p99_ms"`
	LatencyMaxMS int64 `json:"latency_max_ms"`
	Dropped      int64 `json:"dropped"`
	Enqueued     int64 `json:"enqueued"`
	Processed    int64 `json:"processed"`
	Failed       int64 `json:"failed"`
}

type PromptAuditRuntimeSnapshot struct {
	ProcessStatus         string                            `json:"process_status"`
	EffectiveMode         setting.PromptAuditMode           `json:"effective_mode"`
	ExpectedConfigVersion int64                             `json:"expected_config_version"`
	ActiveConfigVersion   int64                             `json:"active_config_version"`
	ConfigLoadedAt        *time.Time                        `json:"config_loaded_at,omitempty"`
	ConfigLoadError       string                            `json:"config_load_error,omitempty"`
	WorkerTotal           int                               `json:"worker_total"`
	WorkerActive          int64                             `json:"worker_active"`
	WorkerHeartbeatAt     *time.Time                        `json:"worker_heartbeat_at,omitempty"`
	LastProcessedAt       *time.Time                        `json:"last_processed_at,omitempty"`
	QueueCapacity         int                               `json:"queue_capacity"`
	Queue                 model.PromptAuditQueueStats       `json:"queue"`
	DatabaseStatus        string                            `json:"database_status"`
	RedisStatus           string                            `json:"redis_status"`
	LastErrorCode         string                            `json:"last_error_code,omitempty"`
	LastErrorMessage      string                            `json:"last_error_message,omitempty"`
	Endpoints             map[string]PromptAuditProbeResult `json:"endpoints"`
	GuardMetrics          PromptAuditMetricsSnapshot        `json:"guard_metrics"`
}

type promptAuditMetrics struct {
	total, allowed, flagged, blocked, unavailable, invalid    atomic.Int64
	timeouts, failovers, dropped, enqueued, processed, failed atomic.Int64
	bulkheadFull, recordFailed, latencyTotal, latencyMax      atomic.Int64
	workerActive, heartbeatNS, lastProcessedNS                atomic.Int64
	latencyMu                                                 sync.RWMutex
	latencies                                                 []int64
	latencyNext                                               int
}

var promptAuditRuntime = struct {
	once          sync.Once
	lifecycleMu   sync.Mutex
	background    context.Context
	cancel        context.CancelFunc
	waitGroup     sync.WaitGroup
	enqueueSlots  chan struct{}
	claimGate     chan struct{}
	nextClaimNS   atomic.Int64
	started       atomic.Bool
	stopping      atomic.Bool
	configStateMu sync.RWMutex
	expected      int64
	loadedAt      time.Time
	loadError     string
	lastErrorCode string
	lastError     string
	metrics       promptAuditMetrics
	probeMu       sync.RWMutex
	probes        map[string]PromptAuditProbeResult
}{
	enqueueSlots: make(chan struct{}, promptAuditEnqueueSlots),
	claimGate: func() chan struct{} {
		gate := make(chan struct{}, 1)
		gate <- struct{}{}
		return gate
	}(),
	probes: map[string]PromptAuditProbeResult{},
}

func StartPromptAuditService() {
	promptAuditRuntime.once.Do(func() {
		background, cancel := context.WithCancel(context.Background())
		promptAuditRuntime.lifecycleMu.Lock()
		promptAuditRuntime.background = background
		promptAuditRuntime.cancel = cancel
		promptAuditRuntime.started.Store(true)
		promptAuditRuntime.lifecycleMu.Unlock()
		storage, err := setting.GetPromptAuditStorageConfig()
		if err != nil {
			recordPromptAuditConfigLoad(0, err)
		} else {
			recordPromptAuditConfigLoad(storage.ConfigVersion, nil)
		}
		for workerID := 0; workerID < setting.MaxPromptAuditWorkerCount; workerID++ {
			promptAuditRuntime.waitGroup.Add(1)
			go func(id int) {
				defer promptAuditRuntime.waitGroup.Done()
				promptAuditWorker(background, id)
			}(workerID)
		}
		promptAuditRuntime.waitGroup.Add(1)
		go func() {
			defer promptAuditRuntime.waitGroup.Done()
			promptAuditReclaimer(background)
		}()
		if common.RedisEnabled && common.RDB != nil {
			promptAuditRuntime.waitGroup.Add(1)
			go func() {
				defer promptAuditRuntime.waitGroup.Done()
				promptAuditConfigSubscriber(background)
			}()
		}
	})
}

func StopPromptAuditService(ctx context.Context) error {
	if !promptAuditRuntime.started.Load() {
		return nil
	}
	promptAuditRuntime.lifecycleMu.Lock()
	promptAuditRuntime.stopping.Store(true)
	cancel := promptAuditRuntime.cancel
	promptAuditRuntime.cancel = nil
	promptAuditRuntime.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		promptAuditRuntime.waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
		promptAuditRuntime.started.Store(false)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func EvaluatePromptAuditRequest(ctx context.Context, config setting.PromptAuditConfig, request PromptAuditRequest) (*PromptAuditDecision, error) {
	if config.Mode != setting.PromptAuditModeBlocking {
		return nil, nil
	}
	snapshot, err := ExtractPromptAuditSnapshot(request, config.BlockingLatestTurnOnly, promptAuditMinimumInputLimit(config))
	if errors.Is(err, ErrNoPromptAuditText) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decision, err := evaluatePromptAuditExtracted(ctx, config, snapshot.ScanText, nil)
	observePromptAuditDecision(decision, err)
	if err != nil {
		return nil, err
	}
	snapshotJSON, snapshotErr := promptAuditRedactedSnapshotJSON(snapshot)
	job := &model.PromptAuditJob{
		ExecutionMode: "blocking", ConfigVersion: config.ConfigVersion,
		RequestId: snapshot.RequestID, UserId: snapshot.UserID, TokenId: snapshot.TokenID,
		GroupName: snapshot.Group, PromptHash: snapshot.PromptHash, SnapshotJSON: snapshotJSON,
	}
	recordErr := snapshotErr
	if recordErr == nil {
		recordErr = model.RecordBlockingPromptAuditResult(
			job,
			promptAuditEventFromSnapshot(snapshot, decision, config.ConfigVersion, nil),
			shouldStorePromptAuditEvent(decision, config),
		)
	}
	if recordErr != nil {
		promptAuditRuntime.metrics.recordFailed.Add(1)
		logger.LogWarn(ctx, "prompt audit blocking event record failed")
	}
	return decision, nil
}

func EnqueuePromptAuditRequest(ctx context.Context, config setting.PromptAuditConfig, request PromptAuditRequest) {
	if config.Mode != setting.PromptAuditModeAsync {
		return
	}
	if !common.RedisEnabled || common.RDB == nil {
		promptAuditRuntime.metrics.dropped.Add(1)
		logger.LogWarn(ctx, "prompt audit async request dropped: redis unavailable")
		return
	}
	select {
	case promptAuditRuntime.enqueueSlots <- struct{}{}:
	default:
		promptAuditRuntime.metrics.dropped.Add(1)
		logPromptAudit(ctx, true, "prompt_audit_enqueue_dropped", map[string]any{"request_id": request.RequestID, "error_code": "local_enqueue_busy", "status": "dropped"})
		return
	}
	promptAuditRuntime.lifecycleMu.Lock()
	background := promptAuditRuntime.background
	if background == nil || promptAuditRuntime.stopping.Load() {
		promptAuditRuntime.lifecycleMu.Unlock()
		<-promptAuditRuntime.enqueueSlots
		promptAuditRuntime.metrics.dropped.Add(1)
		return
	}
	request.Body = append([]byte(nil), request.Body...)
	config.Scanners = append([]string(nil), config.Scanners...)
	config.Endpoints = append([]setting.PromptAuditEndpoint(nil), config.Endpoints...)
	promptAuditRuntime.waitGroup.Add(1)
	promptAuditRuntime.lifecycleMu.Unlock()
	go func() {
		defer promptAuditRuntime.waitGroup.Done()
		defer func() { <-promptAuditRuntime.enqueueSlots }()
		enqueueCtx, cancel := context.WithTimeout(background, 2*time.Second)
		defer cancel()
		enqueuePromptAuditRequest(enqueueCtx, config, request)
	}()
}

func enqueuePromptAuditRequest(enqueueCtx context.Context, config setting.PromptAuditConfig, request PromptAuditRequest) {
	snapshot, err := ExtractPromptAuditSnapshot(request, config.BlockingLatestTurnOnly, promptAuditMinimumInputLimit(config))
	if errors.Is(err, ErrNoPromptAuditText) {
		return
	}
	if err != nil {
		promptAuditRuntime.metrics.dropped.Add(1)
		return
	}
	snapshotJSON, err := promptAuditRedactedSnapshotJSON(snapshot)
	if err != nil {
		promptAuditRuntime.metrics.dropped.Add(1)
		return
	}
	job := &model.PromptAuditJob{
		ExecutionMode: "async_audit",
		ConfigVersion: config.ConfigVersion, RequestId: snapshot.RequestID, UserId: snapshot.UserID,
		TokenId: snapshot.TokenID, GroupName: snapshot.Group, PromptHash: snapshot.PromptHash, SnapshotJSON: string(snapshotJSON),
	}
	if err := model.CreatePromptAuditJobContext(enqueueCtx, job, config.QueueCapacity); err != nil {
		promptAuditRuntime.metrics.dropped.Add(1)
		logPromptAudit(enqueueCtx, true, "prompt_audit_enqueue_failed", map[string]any{"request_id": request.RequestID, "error_code": "job_create_failed", "status": "failed"})
		return
	}
	key := model.PromptAuditJobPayloadKey(job.Id)
	if err := common.RDB.Set(enqueueCtx, key, snapshot.ScanText, promptAuditPayloadTTL).Err(); err != nil {
		_ = model.MarkPromptAuditJobFailedContext(enqueueCtx, job.Id, "payload_store_failed", "prompt audit payload store unavailable")
		promptAuditRuntime.metrics.dropped.Add(1)
		logPromptAudit(enqueueCtx, true, "prompt_audit_enqueue_failed", map[string]any{"job_id": job.Id, "request_id": request.RequestID, "error_code": "payload_store_failed", "status": "failed"})
		return
	}
	if err := model.PublishPromptAuditJobContext(enqueueCtx, job.Id); err != nil {
		_ = common.RDB.Del(enqueueCtx, key).Err()
		_ = model.MarkPromptAuditJobFailedContext(enqueueCtx, job.Id, "queue_publish_failed", "prompt audit queue publish failed")
		promptAuditRuntime.metrics.dropped.Add(1)
		logPromptAudit(enqueueCtx, true, "prompt_audit_enqueue_failed", map[string]any{"job_id": job.Id, "request_id": request.RequestID, "error_code": "queue_publish_failed", "status": "failed"})
		return
	}
	promptAuditRuntime.metrics.enqueued.Add(1)
	promptAuditRuntime.nextClaimNS.Store(0)
	logPromptAudit(enqueueCtx, false, "prompt_audit_job_enqueued", map[string]any{"job_id": job.Id, "request_id": request.RequestID, "config_version": config.ConfigVersion, "status": "queued"})
}

func evaluatePromptAuditExtracted(ctx context.Context, config setting.PromptAuditConfig, text string, client *http.Client) (*PromptAuditDecision, error) {
	return evaluatePromptAuditExtractedWithLease(ctx, config, text, client, nil)
}

func evaluatePromptAuditExtractedWithLease(ctx context.Context, config setting.PromptAuditConfig, text string, client *http.Client, refreshLease func() error) (*PromptAuditDecision, error) {
	if err := setting.ValidatePromptAuditConfig(config); err != nil {
		return nil, fmt.Errorf("%w: invalid configuration", ErrPromptAuditUnavailable)
	}
	chunks := splitPromptAuditRunes(text, promptAuditMinimumInputLimit(config))
	if len(chunks) == 0 {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, promptAuditOverallTimeout(config))
	defer cancel()
	started := time.Now()
	combined := &PromptAuditDecision{}
	categorySet, matchedSet, unknownSet := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for index, chunk := range chunks {
		if refreshLease != nil {
			if err := refreshLease(); err != nil {
				return nil, &promptAuditGuardError{code: "job_lease_lost", retryable: true, cause: err}
			}
		}
		chunkStarted := time.Now()
		logPromptAudit(ctx, false, "prompt_audit_chunk_started", map[string]any{
			"chunk_index": index + 1, "chunk_total": len(chunks), "chunk_chars": len([]rune(chunk)),
			"input_limit": promptAuditMinimumInputLimit(config), "config_version": config.ConfigVersion, "status": "started",
		})
		decision, err := scanPromptAuditChunkWithFailover(timeoutCtx, config, chunk, client)
		if err != nil {
			logPromptAudit(ctx, true, "prompt_audit_chunk_failed", map[string]any{
				"chunk_index": index + 1, "chunk_total": len(chunks), "chunk_chars": len([]rune(chunk)),
				"latency_ms": time.Since(chunkStarted).Milliseconds(), "error_code": promptAuditErrorCode(err), "status": "failed",
			})
			return nil, err
		}
		logPromptAudit(ctx, false, "prompt_audit_chunk_completed", map[string]any{
			"chunk_index": index + 1, "chunk_total": len(chunks), "chunk_chars": len([]rune(chunk)),
			"guard_endpoint_id": decision.GuardEndpointID, "action": decision.Action,
			"latency_ms": time.Since(chunkStarted).Milliseconds(), "status": "completed",
		})
		mergePromptAuditDecision(combined, decision, categorySet, matchedSet, unknownSet)
		if combined.Blocked {
			break
		}
	}
	combined.Categories = orderedPromptAuditCategories(categorySet)
	combined.MatchedScanners = orderedPromptAuditCategories(matchedSet)
	combined.UnknownCategories = sortedPromptAuditKeys(unknownSet)
	combined.ChunkTotal = len(chunks)
	combined.LatencyMS = time.Since(started).Milliseconds()
	logPromptAudit(ctx, false, "prompt_audit_chunks_aggregated", map[string]any{
		"chunk_total": combined.ChunkTotal, "decision": combined.Decision, "risk_level": combined.RiskLevel,
		"action": combined.Action, "guard_endpoint_id": combined.GuardEndpointID, "latency_ms": combined.LatencyMS, "status": "completed",
	})
	return combined, nil
}

func promptAuditWorker(ctx context.Context, workerID int) {
	ticker := time.NewTicker(promptAuditWorkerPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		promptAuditRuntime.metrics.heartbeatNS.Store(time.Now().UnixNano())
		storage, err := setting.GetPromptAuditStorageConfig()
		if err != nil {
			recordPromptAuditRuntimeError("config_invalid", "prompt audit configuration is invalid")
			continue
		}
		if !promptAuditAsyncWorkersEnabled(storage) || workerID >= storage.WorkerCount {
			continue
		}
		for {
			if ctx.Err() != nil {
				return
			}
			job, claimed, err := claimPromptAuditJob(ctx)
			if err != nil {
				recordPromptAuditRuntimeError("job_claim_failed", "prompt audit job claim failed")
				break
			}
			if !claimed {
				break
			}
			promptAuditRuntime.metrics.workerActive.Add(1)
			func() {
				defer promptAuditRuntime.metrics.workerActive.Add(-1)
				defer func() {
					if recovered := recover(); recovered != nil {
						_ = model.RetryPromptAuditJob(job, "worker_panic", "prompt audit worker failed", true)
						promptAuditRuntime.metrics.failed.Add(1)
						recordPromptAuditRuntimeError("worker_panic", "prompt audit worker failed")
						logPromptAudit(ctx, true, "prompt_audit_job_failed", map[string]any{"job_id": job.Id, "error_code": "worker_panic", "status": "failed"})
					}
				}()
				promptAuditProcessJob(ctx, job)
			}()
		}
	}
}

func claimPromptAuditJob(ctx context.Context) (*model.PromptAuditJob, bool, error) {
	// Serialize the short claim operation within this process. When one worker
	// observes an empty queue, the timestamp also coalesces the other workers'
	// empty polls instead of sending an identical query from every worker.
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-promptAuditRuntime.claimGate:
	}
	defer func() { promptAuditRuntime.claimGate <- struct{}{} }()

	now := time.Now()
	if next := promptAuditRuntime.nextClaimNS.Load(); next > now.UnixNano() {
		return nil, false, nil
	}
	job, claimed, err := model.ClaimPromptAuditJobContext(ctx, promptAuditJobLease)
	if err != nil || !claimed {
		promptAuditRuntime.nextClaimNS.Store(time.Now().Add(promptAuditWorkerPoll).UnixNano())
		return job, claimed, err
	}
	promptAuditRuntime.nextClaimNS.Store(0)
	return job, true, nil
}

func promptAuditAsyncWorkersEnabled(storage setting.PromptAuditStorageConfig) bool {
	if !common.IsMasterNode {
		return false
	}
	if storage.Mode == setting.PromptAuditModeAsync {
		return true
	}
	for _, policy := range storage.GroupPolicies {
		if policy.Mode == setting.PromptAuditModeAsync {
			return true
		}
	}
	return false
}

func promptAuditRuntimeEnabled(storage setting.PromptAuditStorageConfig) bool {
	if storage.Mode != setting.PromptAuditModeOff {
		return true
	}
	for _, policy := range storage.GroupPolicies {
		if policy.Mode != setting.PromptAuditModeOff {
			return true
		}
	}
	return false
}

func promptAuditProcessJob(parent context.Context, job *model.PromptAuditJob) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	logPromptAudit(ctx, false, "prompt_audit_job_started", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "attempt": job.Attempts, "status": "processing"})
	var snapshot PromptAuditSnapshot
	if err := json.Unmarshal([]byte(job.SnapshotJSON), &snapshot); err != nil {
		_ = model.RetryPromptAuditJob(job, "snapshot_invalid", "prompt audit snapshot invalid", false)
		promptAuditRuntime.metrics.failed.Add(1)
		recordPromptAuditRuntimeError("snapshot_invalid", "prompt audit snapshot is invalid")
		logPromptAudit(ctx, true, "prompt_audit_job_failed", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "error_code": "snapshot_invalid", "status": "failed"})
		return
	}
	payload, err := common.RDB.Get(ctx, model.PromptAuditJobPayloadKey(job.Id)).Result()
	if err != nil {
		_ = model.RetryPromptAuditJob(job, "payload_missing", "prompt audit payload missing", false)
		promptAuditRuntime.metrics.failed.Add(1)
		recordPromptAuditRuntimeError("payload_missing", "prompt audit payload is unavailable")
		logPromptAudit(ctx, true, "prompt_audit_job_failed", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "error_code": "payload_missing", "status": "failed"})
		return
	}
	snapshot.ScanText = payload
	snapshot.FullPrompt = PromptAuditFullPromptFromScanText(payload)
	config := setting.GetPromptAuditConfigForGroup(snapshot.Group)
	if len(config.Endpoints) == 0 {
		_ = model.RetryPromptAuditJob(job, "no_enabled_endpoint", "no enabled prompt audit endpoint", true)
		promptAuditRuntime.metrics.failed.Add(1)
		recordPromptAuditRuntimeError("no_enabled_endpoint", "no enabled prompt audit endpoint")
		logPromptAudit(ctx, true, "prompt_audit_job_failed", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "error_code": "no_enabled_endpoint", "status": "failed"})
		return
	}
	decision, scanErr := evaluatePromptAuditExtractedWithLease(ctx, config, payload, nil, func() error {
		return model.RefreshPromptAuditJobLease(job.Id, job.ClaimToken, promptAuditJobLease)
	})
	observePromptAuditDecision(decision, scanErr)
	if scanErr != nil {
		var guardErr *promptAuditGuardError
		retryable := errors.As(scanErr, &guardErr) && guardErr.retryable
		_ = model.RetryPromptAuditJob(job, promptAuditErrorCode(scanErr), scanErr.Error(), retryable)
		promptAuditRuntime.metrics.failed.Add(1)
		code := promptAuditErrorCode(scanErr)
		recordPromptAuditRuntimeError(code, "prompt audit guard request failed")
		logPromptAudit(ctx, true, "prompt_audit_job_failed", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "error_code": code, "status": "failed"})
		return
	}
	event := promptAuditEventFromSnapshot(snapshot, decision, job.ConfigVersion, &job.Id)
	store := shouldStorePromptAuditEvent(decision, config)
	if err := model.CompletePromptAuditJob(job, event, store); err != nil {
		promptAuditRuntime.metrics.recordFailed.Add(1)
		_ = model.RetryPromptAuditJob(job, "result_record_failed", "prompt audit result record failed", true)
		promptAuditRuntime.metrics.failed.Add(1)
		recordPromptAuditRuntimeError("result_record_failed", "prompt audit result record failed")
		logPromptAudit(ctx, true, "prompt_audit_job_failed", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "error_code": "result_record_failed", "status": "failed"})
		return
	}
	_ = common.RDB.Del(ctx, model.PromptAuditJobPayloadKey(job.Id)).Err()
	promptAuditRuntime.metrics.processed.Add(1)
	promptAuditRuntime.metrics.lastProcessedNS.Store(time.Now().UnixNano())
	decisionName, latencyMS := "pass", int64(0)
	if decision != nil {
		decisionName, latencyMS = decision.Decision, decision.LatencyMS
	}
	logPromptAudit(ctx, false, "prompt_audit_job_completed", map[string]any{"job_id": job.Id, "request_id": job.RequestId, "decision": decisionName, "latency_ms": latencyMS, "status": "completed"})
}

func shouldStorePromptAuditEvent(decision *PromptAuditDecision, config setting.PromptAuditConfig) bool {
	if decision == nil {
		return false
	}
	if config.StoreBlockedEventsOnly {
		return decision.Blocked
	}
	return decision.Decision != "pass" || config.StorePassEvents
}

func promptAuditReclaimer(ctx context.Context) {
	if !common.IsMasterNode {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := model.ReclaimPromptAuditJobs(); err != nil {
				recordPromptAuditRuntimeError("job_reclaim_failed", "prompt audit job reclaim failed")
			}
		}
	}
}

func promptAuditConfigSubscriber(ctx context.Context) {
	pubsub := common.RDB.Subscribe(ctx, "newapi:prompt_audit:config:invalidate")
	defer pubsub.Close()
	channel := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-channel:
			if !ok {
				return
			}
			version, _ := strconv.ParseInt(strings.TrimSpace(message.Payload), 10, 64)
			if version > 0 {
				recordPromptAuditExpectedVersion(version)
			}
			if err := model.ReloadOption(setting.PromptAuditConfigOptionKey); err != nil {
				recordPromptAuditConfigLoad(version, err)
				continue
			}
			storage, err := setting.GetPromptAuditStorageConfig()
			if err == nil && version > 0 && storage.ConfigVersion != version {
				err = fmt.Errorf("prompt audit config version mismatch")
			}
			if err != nil {
				recordPromptAuditConfigLoad(version, err)
			} else {
				recordPromptAuditConfigLoad(storage.ConfigVersion, nil)
			}
		}
	}
}

func PublishPromptAuditConfigInvalidation(ctx context.Context, version int64) {
	recordPromptAuditExpectedVersion(version)
	recordPromptAuditConfigLoad(version, nil)
	if common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Publish(ctx, "newapi:prompt_audit:config:invalidate", strconv.FormatInt(version, 10)).Err()
	}
}

func DeletePromptAuditJobPayloads(ctx context.Context, jobIDs []int64) {
	if len(jobIDs) == 0 || !common.RedisEnabled || common.RDB == nil {
		return
	}
	keys := make([]string, 0, len(jobIDs))
	seen := make(map[int64]struct{}, len(jobIDs))
	for _, id := range jobIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		keys = append(keys, model.PromptAuditJobPayloadKey(id))
	}
	if len(keys) > 0 {
		_ = common.RDB.Del(ctx, keys...).Err()
	}
}

func recordPromptAuditExpectedVersion(version int64) {
	if version < 1 {
		return
	}
	promptAuditRuntime.configStateMu.Lock()
	if version > promptAuditRuntime.expected {
		promptAuditRuntime.expected = version
	}
	promptAuditRuntime.configStateMu.Unlock()
}

func recordPromptAuditConfigLoad(version int64, err error) {
	promptAuditRuntime.configStateMu.Lock()
	defer promptAuditRuntime.configStateMu.Unlock()
	if version > promptAuditRuntime.expected {
		promptAuditRuntime.expected = version
	}
	if err != nil {
		promptAuditRuntime.loadError = "prompt audit configuration reload failed"
		promptAuditRuntime.lastErrorCode = "config_load_failed"
		promptAuditRuntime.lastError = promptAuditRuntime.loadError
		return
	}
	promptAuditRuntime.loadedAt = time.Now().UTC()
	promptAuditRuntime.loadError = ""
	if promptAuditRuntime.lastErrorCode == "config_load_failed" {
		promptAuditRuntime.lastErrorCode = ""
		promptAuditRuntime.lastError = ""
	}
}

func recordPromptAuditRuntimeError(code, message string) {
	promptAuditRuntime.configStateMu.Lock()
	promptAuditRuntime.lastErrorCode = strings.TrimSpace(code)
	promptAuditRuntime.lastError = strings.TrimSpace(message)
	promptAuditRuntime.configStateMu.Unlock()
}

func promptAuditRuntimeState() (int64, *time.Time, string, string, string) {
	promptAuditRuntime.configStateMu.RLock()
	defer promptAuditRuntime.configStateMu.RUnlock()
	var loadedAt *time.Time
	if !promptAuditRuntime.loadedAt.IsZero() {
		value := promptAuditRuntime.loadedAt
		loadedAt = &value
	}
	return promptAuditRuntime.expected, loadedAt, promptAuditRuntime.loadError,
		promptAuditRuntime.lastErrorCode, promptAuditRuntime.lastError
}

func logPromptAudit(ctx context.Context, warning bool, event string, fields map[string]any) {
	data := make(map[string]any, len(fields)+1)
	data["event"] = event
	for key, value := range fields {
		data[key] = value
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return
	}
	if warning {
		logger.LogWarn(ctx, string(encoded))
	} else {
		logger.LogInfo(ctx, string(encoded))
	}
}

func GetPromptAuditRuntime() PromptAuditRuntimeSnapshot {
	storage, configErr := setting.GetPromptAuditStorageConfig()
	runtimeEnabled := promptAuditRuntimeEnabled(storage)
	asyncEnabled := promptAuditAsyncWorkersEnabled(storage)
	expected, loadedAt, loadError, lastErrorCode, lastError := promptAuditRuntimeState()
	if configErr == nil && storage.ConfigVersion > expected {
		recordPromptAuditConfigLoad(storage.ConfigVersion, nil)
		expected, loadedAt, loadError, lastErrorCode, lastError = promptAuditRuntimeState()
	}
	runtime := PromptAuditRuntimeSnapshot{
		ProcessStatus: "running", EffectiveMode: storage.Mode, ExpectedConfigVersion: expected,
		ActiveConfigVersion: storage.ConfigVersion, ConfigLoadedAt: loadedAt, ConfigLoadError: loadError,
		WorkerTotal: storage.WorkerCount, WorkerActive: promptAuditRuntime.metrics.workerActive.Load(),
		QueueCapacity: storage.QueueCapacity, DatabaseStatus: "ok", RedisStatus: "ok", Endpoints: promptAuditProbeSnapshot(),
		GuardMetrics: promptAuditMetricsValue(), LastErrorCode: lastErrorCode, LastErrorMessage: lastError,
	}
	if configErr != nil {
		runtime.ProcessStatus, runtime.LastErrorCode, runtime.LastErrorMessage = "degraded", "config_invalid", "prompt audit configuration is invalid"
	}
	queue, err := model.PromptAuditQueueStatistics()
	if err != nil {
		runtime.DatabaseStatus, runtime.ProcessStatus = "error", "degraded"
	} else {
		runtime.Queue = queue
	}
	if !common.RedisEnabled || common.RDB == nil {
		runtime.RedisStatus = "disabled"
		if asyncEnabled {
			runtime.ProcessStatus = "degraded"
		}
	} else {
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := common.RDB.Ping(pingCtx).Err()
		cancel()
		if err != nil {
			runtime.RedisStatus, runtime.ProcessStatus = "error", "degraded"
		}
	}
	if value := promptAuditRuntime.metrics.heartbeatNS.Load(); value > 0 {
		t := time.Unix(0, value)
		runtime.WorkerHeartbeatAt = &t
	}
	if value := promptAuditRuntime.metrics.lastProcessedNS.Load(); value > 0 {
		t := time.Unix(0, value)
		runtime.LastProcessedAt = &t
	}
	if !runtimeEnabled {
		runtime.ProcessStatus = "disabled"
	} else if runtime.ExpectedConfigVersion != runtime.ActiveConfigVersion || runtime.ConfigLoadError != "" {
		runtime.ProcessStatus = "degraded"
	}
	if runtimeEnabled && (runtime.WorkerHeartbeatAt == nil || time.Since(*runtime.WorkerHeartbeatAt) > 10*time.Second) {
		runtime.ProcessStatus = "degraded"
	}
	if !promptAuditRuntime.started.Load() && runtimeEnabled {
		runtime.ProcessStatus = "stopped"
	}
	return runtime
}

func ProbePromptAuditEndpoint(ctx context.Context, endpoint setting.PromptAuditEndpoint, token string, scanners []string) PromptAuditProbeResult {
	started := time.Now()
	result := PromptAuditProbeResult{Status: "failed", Message: "prompt audit endpoint probe failed", CheckedAt: time.Now()}
	result.TokenApplied = strings.TrimSpace(token) != "" || endpoint.TokenCiphertext != ""
	config := setting.PromptAuditConfig{
		Enabled: true, Mode: setting.PromptAuditModeBlocking, MaxConcurrency: 1, Scanners: scanners,
		Endpoints: []setting.PromptAuditEndpoint{endpoint}, BaseURL: endpoint.BaseURL, Model: endpoint.Model,
		TimeoutMS: endpoint.TimeoutMS, InputLimit: endpoint.InputLimit,
	}
	if strings.TrimSpace(token) != "" {
		config.APIKey = strings.TrimSpace(token)
	}
	resolvedToken := strings.TrimSpace(token)
	if resolvedToken == "" && endpoint.TokenCiphertext != "" {
		resolvedToken, _ = setting.DecryptPromptAuditToken(endpoint.TokenCiphertext)
	}
	client, clientErr := promptAuditHTTPClient(endpoint)
	if clientErr != nil {
		result.ErrorCode = "endpoint_invalid"
		result.LatencyMS, result.CheckedAt = time.Since(started).Milliseconds(), time.Now()
		return finishPromptAuditProbe(endpoint.ID, result)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, promptAuditOverallTimeout(config))
	defer cancel()
	modelsURL, _ := PromptAuditModelsURL(endpoint.BaseURL)
	request, requestErr := http.NewRequestWithContext(timeoutCtx, http.MethodGet, modelsURL, nil)
	if requestErr == nil && resolvedToken != "" {
		request.Header.Set("Authorization", "Bearer "+resolvedToken)
	}
	var modelsStatus int
	var modelsReady bool
	var modelsFallback bool
	var probeErr error
	if requestErr != nil {
		probeErr = requestErr
	} else if response, err := client.Do(request); err != nil {
		probeErr = err
	} else {
		modelsStatus = response.StatusCode
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxPromptAuditResponseBytes+1))
		_ = response.Body.Close()
		if readErr != nil {
			probeErr = readErr
		} else if len(body) > maxPromptAuditResponseBytes {
			result.ErrorCode = "response_too_large"
		} else if response.StatusCode >= 200 && response.StatusCode < 300 {
			modelsReady = promptAuditModelsResponseReady(body, endpoint.Model)
			modelsFallback = !modelsReady
		} else if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
			modelsFallback = true
		} else {
			result.ErrorCode = "probe_http_error"
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
				result.ErrorCode = "authentication_failed"
			}
			result.Retryable = response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		}
	}
	var decision *PromptAuditDecision
	var err error
	if modelsReady {
		result.OK, result.Status, result.Message, result.HTTPStatus = true, "healthy", "prompt audit endpoint is available", modelsStatus
	} else if modelsFallback {
		decision, err = scanPromptAuditChunkWithFailover(timeoutCtx, config, "Please classify this benign connection probe.", client)
	} else if probeErr != nil {
		err = &promptAuditGuardError{code: "connection_failed", retryable: true, cause: probeErr}
		var netErr net.Error
		if errors.Is(probeErr, context.DeadlineExceeded) || (errors.As(probeErr, &netErr) && netErr.Timeout()) {
			err = &promptAuditGuardError{code: "timeout", retryable: true, timeout: true, cause: probeErr}
		}
	}
	result.LatencyMS, result.CheckedAt = time.Since(started).Milliseconds(), time.Now()
	if result.OK {
		// The models endpoint already proved readiness.
	} else if err == nil && decision != nil {
		result.OK, result.Status, result.Message, result.HTTPStatus = true, "healthy", "prompt audit endpoint is available", http.StatusOK
	} else {
		if result.ErrorCode == "" {
			result.ErrorCode = promptAuditErrorCode(err)
		}
		var guardErr *promptAuditGuardError
		if errors.As(err, &guardErr) {
			result.HTTPStatus, result.Retryable = guardErr.httpStatus, guardErr.retryable
		}
		if result.HTTPStatus == 0 {
			result.HTTPStatus = modelsStatus
		}
	}
	return finishPromptAuditProbe(endpoint.ID, result)
}

func finishPromptAuditProbe(endpointID string, result PromptAuditProbeResult) PromptAuditProbeResult {
	promptAuditRuntime.probeMu.Lock()
	promptAuditRuntime.probes[endpointID] = result
	promptAuditRuntime.probeMu.Unlock()
	return result
}

func promptAuditModelsResponseReady(body []byte, model string) bool {
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.Data == nil {
		return false
	}
	for _, item := range response.Data {
		if item.ID == model {
			return true
		}
	}
	return false
}

func promptAuditProbeSnapshot() map[string]PromptAuditProbeResult {
	promptAuditRuntime.probeMu.RLock()
	defer promptAuditRuntime.probeMu.RUnlock()
	result := make(map[string]PromptAuditProbeResult, len(promptAuditRuntime.probes))
	for key, value := range promptAuditRuntime.probes {
		result[key] = value
	}
	return result
}

func promptAuditMetricsValue() PromptAuditMetricsSnapshot {
	m := &promptAuditRuntime.metrics
	snapshot := PromptAuditMetricsSnapshot{
		Total: m.total.Load(), Allowed: m.allowed.Load(), Flagged: m.flagged.Load(), Blocked: m.blocked.Load(),
		Unavailable: m.unavailable.Load(), Invalid: m.invalid.Load(), Timeouts: m.timeouts.Load(), Failovers: m.failovers.Load(),
		BulkheadFull: m.bulkheadFull.Load(), RecordFailed: m.recordFailed.Load(), LatencyCount: m.total.Load(), LatencyMaxMS: m.latencyMax.Load(),
		Dropped: m.dropped.Load(), Enqueued: m.enqueued.Load(), Processed: m.processed.Load(), Failed: m.failed.Load(),
	}
	if snapshot.LatencyCount > 0 {
		snapshot.LatencyAvgMS = m.latencyTotal.Load() / snapshot.LatencyCount
	}
	m.latencyMu.RLock()
	samples := append([]int64(nil), m.latencies...)
	m.latencyMu.RUnlock()
	if len(samples) > 0 {
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		snapshot.LatencyP50MS = promptAuditPercentile(samples, 0.50)
		snapshot.LatencyP95MS = promptAuditPercentile(samples, 0.95)
		snapshot.LatencyP99MS = promptAuditPercentile(samples, 0.99)
	}
	return snapshot
}

func observePromptAuditDecision(decision *PromptAuditDecision, err error) {
	m := &promptAuditRuntime.metrics
	m.total.Add(1)
	latency := int64(0)
	if decision != nil && decision.LatencyMS > 0 {
		latency = decision.LatencyMS
	}
	m.latencyTotal.Add(latency)
	for current := m.latencyMax.Load(); latency > current && !m.latencyMax.CompareAndSwap(current, latency); current = m.latencyMax.Load() {
	}
	m.latencyMu.Lock()
	if len(m.latencies) < promptAuditLatencySamples {
		m.latencies = append(m.latencies, latency)
	} else {
		m.latencies[m.latencyNext] = latency
		m.latencyNext = (m.latencyNext + 1) % promptAuditLatencySamples
	}
	m.latencyMu.Unlock()
	if err != nil {
		var guardErr *promptAuditGuardError
		if errors.As(err, &guardErr) && guardErr.code == "prompt_guard_invalid_response" {
			m.invalid.Add(1)
		} else {
			m.unavailable.Add(1)
		}
		if errors.As(err, &guardErr) && guardErr.timeout {
			m.timeouts.Add(1)
		}
		return
	}
	if decision == nil || decision.Decision == "pass" {
		m.allowed.Add(1)
	} else if decision.Decision == "flag" {
		m.flagged.Add(1)
	} else {
		m.blocked.Add(1)
	}
}

func promptAuditPercentile(sorted []int64, quantile float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * quantile)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func promptAuditRedactedSnapshotJSON(snapshot PromptAuditSnapshot) (string, error) {
	stored := snapshot
	stored.ScanText = ""
	stored.FullPrompt = ""
	raw, err := json.Marshal(stored)
	return string(raw), err
}

func promptAuditErrorCode(err error) string {
	var guardErr *promptAuditGuardError
	if errors.As(err, &guardErr) && guardErr.code != "" {
		return guardErr.code
	}
	return "prompt_guard_unavailable"
}

func promptAuditEventFromSnapshot(snapshot PromptAuditSnapshot, decision *PromptAuditDecision, configVersion int64, jobID *int64) *model.PromptAuditEvent {
	event := &model.PromptAuditEvent{
		JobId: jobID, RequestId: snapshot.RequestID, UserId: snapshot.UserID, Username: snapshot.Username, UserEmail: snapshot.UserEmail,
		TokenId: snapshot.TokenID, TokenName: snapshot.TokenName, GroupName: snapshot.Group,
		Provider: snapshot.Provider, Endpoint: snapshot.Endpoint, Protocol: snapshot.Protocol, Model: snapshot.Model,
		PromptHash: snapshot.PromptHash, RedactedPreview: snapshot.RedactedPreview, FullPrompt: snapshot.FullPrompt,
		PromptLength: snapshot.PromptLength, MessageCount: snapshot.MessageCount, Stage: snapshot.Stage, ConfigVersion: configVersion,
	}
	if decision == nil {
		return event
	}
	event.Decision, event.RiskLevel, event.Action, event.Safety = decision.Decision, decision.RiskLevel, decision.Action, decision.Safety
	event.CategoriesJSON = promptAuditJSON(decision.Categories)
	event.MatchedScannersJSON = promptAuditJSON(decision.MatchedScanners)
	event.UnknownCategoriesJSON = promptAuditJSON(decision.UnknownCategories)
	event.ScannerScoresJSON = promptAuditJSON(decision.ScannerScores)
	event.ScannerEvidenceJSON = promptAuditJSON(decision.ScannerEvidence)
	event.ScannerBackend, event.ScannerVersion = decision.ScannerBackend, decision.ScannerVersion
	event.GuardEndpointId, event.PolicyId, event.PolicyVersion = decision.GuardEndpointID, decision.PolicyID, decision.PolicyVersion
	event.ChunkTotal, event.LatencyMS = decision.ChunkTotal, decision.LatencyMS
	return event
}

func promptAuditJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func DecodePromptAuditEvent(event model.PromptAuditEvent) map[string]any {
	data := map[string]any{}
	raw, _ := json.Marshal(event)
	_ = json.Unmarshal(raw, &data)
	decodeStrings := func(key, value string) []string {
		result := []string{}
		if strings.TrimSpace(value) != "" {
			_ = json.Unmarshal([]byte(value), &result)
		}
		data[key] = result
		return result
	}
	categories := decodeStrings("categories", event.CategoriesJSON)
	matched := decodeStrings("matched_scanners", event.MatchedScannersJSON)
	unknown := decodeStrings("unknown_categories", event.UnknownCategoriesJSON)
	scores := map[string]float64{}
	evidence := map[string]string{}
	_ = json.Unmarshal([]byte(event.ScannerScoresJSON), &scores)
	_ = json.Unmarshal([]byte(event.ScannerEvidenceJSON), &evidence)
	data["scanner_scores"] = scores
	data["scanner_evidence"] = evidence
	data["issue_summaries"] = buildPromptAuditIssueSummaries(categories, matched, unknown, event.RiskLevel, event.Action, scores, evidence)
	return data
}

func buildPromptAuditIssueSummaries(categories, matched, unknown []string, riskLevel, action string, scores map[string]float64, evidence map[string]string) []map[string]any {
	if len(categories) == 0 {
		categories = matched
	}
	result := make([]map[string]any, 0, len(categories)+len(unknown))
	for _, category := range categories {
		definition, ok := promptAuditScannerDefinitions[category]
		if !ok {
			continue
		}
		evidenceText := redactPromptAuditValue(evidence[category], 160)
		if evidenceText == "" {
			evidenceText = definition.Label
		}
		digest := sha256.Sum256([]byte(evidenceText))
		result = append(result, map[string]any{
			"category": category, "scanner_id": category, "title": definition.LabelZH,
			"description": definition.Description, "severity": riskLevel,
			"severity_label": promptAuditRiskLabelZH(riskLevel), "action": action,
			"action_label": promptAuditActionLabelZH(action), "code": "prompt_audit_" + category,
			"score": scores[category], "evidence": evidenceText, "evidence_hash": hex.EncodeToString(digest[:]),
		})
	}
	for _, category := range unknown {
		evidenceText := "unknown_unsafe"
		digest := sha256.Sum256([]byte(evidenceText + ":" + category))
		result = append(result, map[string]any{
			"category": category, "scanner_id": "unknown_unsafe", "title": "未知高风险分类",
			"description": "审计节点返回了未知但不可忽略的高风险分类",
			"severity":    "critical", "severity_label": "严重", "action": "Block", "action_label": "阻止",
			"code": "prompt_audit_unknown_unsafe", "score": 1, "evidence": evidenceText,
			"evidence_hash": hex.EncodeToString(digest[:]),
		})
	}
	return result
}

func promptAuditRiskLabelZH(risk string) string {
	switch risk {
	case "critical":
		return "严重"
	case "high":
		return "高"
	case "medium":
		return "中"
	default:
		return "低"
	}
}

func promptAuditActionLabelZH(action string) string {
	switch action {
	case "Block":
		return "阻止"
	case "Warn":
		return "警告"
	default:
		return "允许"
	}
}
