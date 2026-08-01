package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

const maxPromptAuditResponseBytes = 256 << 10

var ErrPromptAuditUnavailable = errors.New("prompt audit unavailable")

type PromptAuditDecision struct {
	Blocked           bool
	Safety            string
	Categories        []string
	MatchedScanners   []string
	UnknownCategories []string
	Decision          string
	RiskLevel         string
	Action            string
	ScannerScores     map[string]float64
	ScannerEvidence   map[string]string
	ScannerBackend    string
	ScannerVersion    string
	GuardEndpointID   string
	PolicyID          string
	PolicyVersion     int
	ChunkTotal        int
	LatencyMS         int64
}

type promptAuditGuardError struct {
	code       string
	retryable  bool
	timeout    bool
	httpStatus int
	cause      error
}

func (e *promptAuditGuardError) Error() string {
	if e == nil || e.code == "" {
		return "prompt audit guard error"
	}
	return e.code
}

func (e *promptAuditGuardError) Unwrap() error { return e.cause }

var promptAuditCategoryAliases = map[string]string{
	"violent": "violent", "violence": "violent",
	"non violent illegal acts": "non_violent_illegal_acts", "non-violent illegal acts": "non_violent_illegal_acts",
	"sexual content or sexual acts": "sexual_content_or_sexual_acts", "sexual": "sexual_content_or_sexual_acts",
	"pii": "pii", "personal identifying information": "pii", "personal identifiable information": "pii",
	"suicide self harm": "suicide_and_self_harm", "suicide and self harm": "suicide_and_self_harm", "suicide & self-harm": "suicide_and_self_harm",
	"unethical acts": "unethical_acts", "unethical": "unethical_acts",
	"politically sensitive topics": "politically_sensitive_topics", "political": "politically_sensitive_topics",
	"copyright violation": "copyright_violation", "copyright": "copyright_violation",
	"jailbreak": "jailbreak", "prompt injection": "jailbreak",
}

var promptAuditScannerLabels = map[string]string{
	"violent":                       "Violent",
	"non_violent_illegal_acts":      "Non-violent Illegal Acts",
	"sexual_content_or_sexual_acts": "Sexual Content or Sexual Acts",
	"pii":                           "PII",
	"suicide_and_self_harm":         "Suicide & Self-Harm",
	"unethical_acts":                "Unethical Acts",
	"politically_sensitive_topics":  "Politically Sensitive Topics",
	"copyright_violation":           "Copyright Violation",
	"jailbreak":                     "Jailbreak",
}

type promptAuditScannerDefinition struct {
	Label       string
	LabelZH     string
	Description string
}

var promptAuditScannerDefinitions = map[string]promptAuditScannerDefinition{
	"violent":                       {Label: "Violent", LabelZH: "暴力", Description: "Violence or threats of violence"},
	"non_violent_illegal_acts":      {Label: "Non-violent Illegal Acts", LabelZH: "非暴力违法行为", Description: "Non-violent illegal activity"},
	"sexual_content_or_sexual_acts": {Label: "Sexual Content or Sexual Acts", LabelZH: "性内容或性行为", Description: "Sexual content or sexual acts"},
	"pii":                           {Label: "PII", LabelZH: "个人敏感信息", Description: "Personal identifying information"},
	"suicide_and_self_harm":         {Label: "Suicide & Self-Harm", LabelZH: "自杀与自残", Description: "Suicide or self-harm"},
	"unethical_acts":                {Label: "Unethical Acts", LabelZH: "不道德行为", Description: "Unethical behavior"},
	"politically_sensitive_topics":  {Label: "Politically Sensitive Topics", LabelZH: "政治敏感话题", Description: "Politically sensitive topics"},
	"copyright_violation":           {Label: "Copyright Violation", LabelZH: "版权侵权", Description: "Copyright infringement"},
	"jailbreak":                     {Label: "Jailbreak", LabelZH: "越狱攻击", Description: "Prompt injection or jailbreak attempt"},
}

func EvaluatePromptAudit(ctx context.Context, config setting.PromptAuditConfig, protocol string, body []byte) (*PromptAuditDecision, error) {
	if !config.Enabled {
		return nil, nil
	}
	return evaluatePromptAudit(ctx, config, protocol, body, nil)
}

func evaluatePromptAudit(ctx context.Context, config setting.PromptAuditConfig, protocol string, body []byte, client *http.Client) (*PromptAuditDecision, error) {
	if err := setting.ValidatePromptAuditConfig(config); err != nil {
		return nil, fmt.Errorf("%w: invalid configuration", ErrPromptAuditUnavailable)
	}
	inputLimit := promptAuditMinimumInputLimit(config)
	text, err := extractPromptAuditText(protocol, body, inputLimit)
	if errors.Is(err, ErrNoPromptAuditText) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: invalid request", ErrPromptAuditUnavailable)
	}
	chunks := splitPromptAuditRunes(text, inputLimit)
	if len(chunks) == 0 {
		return nil, nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, promptAuditOverallTimeout(config))
	defer cancel()
	started := time.Now()
	combined := &PromptAuditDecision{}
	categorySet := map[string]struct{}{}
	matchedSet := map[string]struct{}{}
	unknownSet := map[string]struct{}{}
	for _, chunk := range chunks {
		decision, scanErr := scanPromptAuditChunkWithFailover(timeoutCtx, config, chunk, client)
		if scanErr != nil {
			return nil, scanErr
		}
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
	return combined, nil
}

func ProbePromptAudit(ctx context.Context, config setting.PromptAuditConfig) error {
	if err := setting.ValidatePromptAuditConfig(config); err != nil {
		return err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, promptAuditOverallTimeout(config))
	defer cancel()
	_, err := scanPromptAuditChunkWithFailover(timeoutCtx, config, "Please classify this benign connection probe.", nil)
	return err
}

func scanPromptAuditChunkWithFailover(ctx context.Context, config setting.PromptAuditConfig, chunk string, client *http.Client) (*PromptAuditDecision, error) {
	endpoints := config.Endpoints
	if len(endpoints) == 0 && config.BaseURL != "" {
		endpoints = []setting.PromptAuditEndpoint{{
			ID: "legacy", Name: "Qwen3Guard", Protocol: "openai_compatible", BaseURL: config.BaseURL,
			Model: config.Model, TimeoutMS: config.TimeoutMS, InputLimit: config.InputLimit, Enabled: true,
		}}
	}
	var lastErr error
	for index, endpoint := range endpoints {
		if !endpoint.Enabled {
			continue
		}
		endpointConfig := config
		endpointConfig.BaseURL = endpoint.BaseURL
		endpointConfig.Model = endpoint.Model
		endpointConfig.TimeoutMS = endpoint.TimeoutMS
		endpointConfig.InputLimit = endpoint.InputLimit
		endpointConfig.Endpoints = nil
		endpointConfig.APIKey = ""
		if endpoint.TokenCiphertext != "" {
			token, err := setting.DecryptPromptAuditToken(endpoint.TokenCiphertext)
			if err != nil {
				lastErr = &promptAuditGuardError{code: "endpoint_token_undecryptable", retryable: true, cause: err}
				if index < len(endpoints)-1 {
					promptAuditRuntime.metrics.failovers.Add(1)
				}
				continue
			}
			endpointConfig.APIKey = token
		} else if config.APIKey != "" {
			endpointConfig.APIKey = config.APIKey
		}
		endpointClient := client
		var err error
		if endpointClient == nil {
			endpointClient, err = promptAuditHTTPClient(endpoint)
			if err != nil {
				lastErr = &promptAuditGuardError{code: "prompt_guard_unavailable", retryable: true, cause: err}
				if index < len(endpoints)-1 {
					promptAuditRuntime.metrics.failovers.Add(1)
				}
				continue
			}
		}
		decision, err := func() (decision *PromptAuditDecision, scanErr error) {
			defer func() {
				if recover() != nil {
					decision = nil
					scanErr = &promptAuditGuardError{code: "prompt_guard_unavailable"}
				}
			}()
			return scanPromptAuditChunk(ctx, endpointConfig, chunk, endpointClient)
		}()
		if err == nil && decision != nil {
			decision.GuardEndpointID = endpoint.ID
			decision.ScannerVersion = endpoint.Model
			return decision, nil
		}
		lastErr = err
		var guardErr *promptAuditGuardError
		if !errors.As(err, &guardErr) || !guardErr.retryable {
			return nil, err
		}
		if index < len(endpoints)-1 {
			promptAuditRuntime.metrics.failovers.Add(1)
		}
	}
	if lastErr == nil {
		lastErr = &promptAuditGuardError{code: "prompt_guard_unavailable"}
	}
	return nil, lastErr
}

func scanPromptAuditChunk(ctx context.Context, config setting.PromptAuditConfig, chunk string, client *http.Client) (*PromptAuditDecision, error) {
	requestURL, err := PromptAuditChatCompletionsURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", ErrPromptAuditUnavailable)
	}
	payload := map[string]any{
		"model":       config.Model,
		"messages":    []map[string]string{{"role": "user", "content": chunk}},
		"temperature": 0,
		"max_tokens":  64,
		"seed":        42,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: could not encode request", ErrPromptAuditUnavailable)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request", ErrPromptAuditUnavailable)
	}
	request.Header.Set("Content-Type", "application/json")
	if config.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+config.APIKey)
	}
	response, err := client.Do(request)
	if err != nil {
		timedOut := errors.Is(err, context.DeadlineExceeded)
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			timedOut = true
		}
		return nil, &promptAuditGuardError{code: "prompt_guard_unavailable", retryable: true, timeout: timedOut, cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &promptAuditGuardError{code: "prompt_guard_unavailable", retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500, httpStatus: response.StatusCode}
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxPromptAuditResponseBytes+1))
	if err != nil || len(responseBody) > maxPromptAuditResponseBytes {
		return nil, &promptAuditGuardError{code: "prompt_guard_invalid_response", cause: err}
	}
	content, err := extractPromptAuditOpenAIContent(responseBody)
	if err != nil {
		return nil, &promptAuditGuardError{code: "prompt_guard_invalid_response", cause: err}
	}
	decision, err := ParseQwenGuardPromptAudit(content, config.Scanners)
	if err != nil {
		return nil, &promptAuditGuardError{code: "prompt_guard_invalid_response", cause: err}
	}
	return decision, nil
}

func PromptAuditChatCompletionsURL(baseURL string) (string, error) {
	if err := setting.ValidatePromptAuditBaseURL(baseURL); err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
	case strings.HasSuffix(path, "/v1"):
		path += "/chat/completions"
	default:
		path += "/v1/chat/completions"
	}
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	parsed.Path = path
	return parsed.String(), nil
}

func PromptAuditModelsURL(baseURL string) (string, error) {
	if err := setting.ValidatePromptAuditBaseURL(baseURL); err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	if strings.HasSuffix(path, "/v1") {
		path += "/models"
	} else {
		path += "/v1/models"
	}
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	parsed.Path = path
	return parsed.String(), nil
}

func ParseQwenGuardPromptAudit(content string, enabledScanners []string) (*PromptAuditDecision, error) {
	var safety string
	var categoryLine string
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "safety:"):
			if safety != "" {
				return nil, errors.New("duplicate safety field")
			}
			safety = strings.TrimSpace(line[len("safety:"):])
		case strings.HasPrefix(lower, "categories:"):
			if categoryLine != "" {
				return nil, errors.New("duplicate categories field")
			}
			categoryLine = strings.TrimSpace(line[len("categories:"):])
		}
	}
	switch strings.ToLower(safety) {
	case "safe":
		safety = "Safe"
	case "controversial":
		safety = "Controversial"
	case "unsafe":
		safety = "Unsafe"
	default:
		return nil, errors.New("invalid safety field")
	}
	if categoryLine == "" {
		return nil, errors.New("missing categories field")
	}

	knownCatalog := make(map[string]struct{}, len(setting.PromptAuditScannerIDs))
	for _, scanner := range setting.PromptAuditScannerIDs {
		knownCatalog[scanner] = struct{}{}
	}
	enabled := make(map[string]struct{}, len(enabledScanners))
	for _, scanner := range enabledScanners {
		enabled[normalizePromptAuditCategory(scanner)] = struct{}{}
	}
	known := map[string]struct{}{}
	unknown := map[string]struct{}{}
	categoryLine = strings.ReplaceAll(categoryLine, ";", ",")
	for _, raw := range strings.Split(categoryLine, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "n/a") {
			continue
		}
		category := normalizePromptAuditCategory(raw)
		if _, ok := knownCatalog[category]; ok {
			known[category] = struct{}{}
		} else {
			unknown[promptAuditUnknownCategoryID(category)] = struct{}{}
		}
	}
	matched := map[string]struct{}{}
	for category := range known {
		if _, ok := enabled[category]; ok {
			matched[category] = struct{}{}
		}
	}
	blocked := false
	decisionKind, riskLevel, action := "pass", "low", "Allow"
	if safety == "Unsafe" {
		blocked = len(matched) > 0 || len(unknown) > 0 || len(known) == 0
		if blocked {
			decisionKind, riskLevel, action = "critical", "critical", "Block"
		} else {
			decisionKind, riskLevel, action = "flag", "high", "Warn"
		}
	}
	if safety == "Controversial" {
		decisionKind, riskLevel, action = "flag", "medium", "Warn"
		for category := range matched {
			if category == "jailbreak" || category == "pii" || category == "suicide_and_self_harm" {
				blocked = true
				decisionKind, riskLevel, action = "critical", "critical", "Block"
				break
			}
		}
	}
	score := 0.0
	if safety == "Controversial" {
		score = 0.5
	} else if safety == "Unsafe" {
		score = 1
	}
	scores := make(map[string]float64, len(matched))
	evidence := make(map[string]string, len(matched))
	for category := range matched {
		scores[category] = score
		evidence[category] = promptAuditScannerLabels[category]
	}
	return &PromptAuditDecision{
		Blocked:           blocked,
		Safety:            safety,
		Categories:        orderedPromptAuditCategories(known),
		MatchedScanners:   orderedPromptAuditCategories(matched),
		UnknownCategories: sortedPromptAuditKeys(unknown),
		Decision:          decisionKind,
		RiskLevel:         riskLevel,
		Action:            action,
		ScannerScores:     scores,
		ScannerEvidence:   evidence,
		ScannerBackend:    "qwen3guard-openai",
		ScannerVersion:    "qwen3guard",
		PolicyID:          "priority",
		PolicyVersion:     1,
	}, nil
}

func promptAuditUnknownCategoryID(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(value))))
	return "unknown:" + hex.EncodeToString(digest[:8])
}

func extractPromptAuditOpenAIContent(body []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &response); err != nil || len(response.Choices) == 0 {
		return "", errors.New("invalid response envelope")
	}
	switch content := response.Choices[0].Message.Content.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return "", errors.New("empty response content")
		}
		return content, nil
	case []any:
		parts := make([]string, 0, len(content))
		for _, item := range content {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text := stringValue(object["text"]); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return "", errors.New("empty response content")
		}
		return strings.Join(parts, "\n"), nil
	default:
		return "", errors.New("invalid response content")
	}
}

func splitPromptAuditRunes(value string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	segments := strings.Split(value, promptAuditPrioritySeparator)
	chunks := make([]string, 0, len(segments))
	for _, segment := range segments {
		runes := []rune(strings.TrimSpace(segment))
		for start := 0; start < len(runes); start += limit {
			end := start + limit
			if end > len(runes) {
				end = len(runes)
			}
			chunks = append(chunks, string(runes[start:end]))
		}
	}
	return chunks
}

func promptAuditMinimumInputLimit(config setting.PromptAuditConfig) int {
	limit := config.InputLimit
	for _, endpoint := range config.Endpoints {
		if !endpoint.Enabled || endpoint.InputLimit <= 0 {
			continue
		}
		if limit <= 0 || endpoint.InputLimit < limit {
			limit = endpoint.InputLimit
		}
	}
	if limit <= 0 {
		limit = setting.DefaultPromptAuditInputLimit
	}
	return limit
}

func promptAuditOverallTimeout(config setting.PromptAuditConfig) time.Duration {
	timeoutMS := config.TimeoutMS
	if timeoutMS <= 0 && len(config.Endpoints) > 0 {
		timeoutMS = config.Endpoints[0].TimeoutMS
	}
	if timeoutMS <= 0 {
		timeoutMS = setting.DefaultPromptAuditTimeoutMS
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func normalizePromptAuditCategory(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("_", " ", "&", " and ", "/", " ", "-", " ", "–", " ", "—", " ").Replace(normalized)
	normalized = strings.Join(strings.Fields(normalized), " ")
	if canonical, ok := promptAuditCategoryAliases[normalized]; ok {
		return canonical
	}
	return strings.ReplaceAll(normalized, " ", "_")
}

func mergePromptAuditDecision(combined *PromptAuditDecision, decision *PromptAuditDecision, categories map[string]struct{}, matched map[string]struct{}, unknown map[string]struct{}) {
	if decision == nil {
		return
	}
	if combined.Safety == "" || promptAuditSafetySeverity(decision.Safety) > promptAuditSafetySeverity(combined.Safety) {
		combined.Safety = decision.Safety
	}
	combined.Blocked = combined.Blocked || decision.Blocked
	if combined.Decision == "" || promptAuditDecisionSeverity(decision.Decision) > promptAuditDecisionSeverity(combined.Decision) {
		combined.Decision = decision.Decision
		combined.RiskLevel = decision.RiskLevel
		combined.Action = decision.Action
		combined.GuardEndpointID = decision.GuardEndpointID
		combined.ScannerVersion = decision.ScannerVersion
	}
	combined.ScannerBackend = decision.ScannerBackend
	combined.PolicyID = decision.PolicyID
	combined.PolicyVersion = decision.PolicyVersion
	if combined.ScannerScores == nil {
		combined.ScannerScores = map[string]float64{}
	}
	if combined.ScannerEvidence == nil {
		combined.ScannerEvidence = map[string]string{}
	}
	for key, value := range decision.ScannerScores {
		if value > combined.ScannerScores[key] {
			combined.ScannerScores[key] = value
		}
	}
	for key, value := range decision.ScannerEvidence {
		combined.ScannerEvidence[key] = value
	}
	for _, category := range decision.Categories {
		categories[category] = struct{}{}
	}
	for _, scanner := range decision.MatchedScanners {
		matched[scanner] = struct{}{}
	}
	for _, category := range decision.UnknownCategories {
		unknown[category] = struct{}{}
	}
}

func promptAuditDecisionSeverity(decision string) int {
	switch decision {
	case "critical":
		return 3
	case "flag":
		return 2
	default:
		return 1
	}
}

func promptAuditSafetySeverity(safety string) int {
	switch safety {
	case "Unsafe":
		return 3
	case "Controversial":
		return 2
	default:
		return 1
	}
}

func orderedPromptAuditCategories(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	remaining := make(map[string]struct{}, len(values))
	for key := range values {
		remaining[key] = struct{}{}
	}
	for _, scanner := range setting.PromptAuditScannerIDs {
		if _, ok := remaining[scanner]; ok {
			result = append(result, scanner)
			delete(remaining, scanner)
		}
	}
	return append(result, sortedPromptAuditKeys(remaining)...)
}

func sortedPromptAuditKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
