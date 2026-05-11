package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

const (
	defaultSessionRecorderQueueSize         = 1024
	defaultSessionRecorderWorkers           = 1
	defaultSessionRecorderMaxCapturedBytes  = 1 << 20
	defaultSessionRecorderRequestTimeoutSec = 3
)

var sessionRecorder struct {
	once   sync.Once
	client *sessionRecorderClient
}

type sessionRecorderClient struct {
	endpoint         string
	nodeName         string
	maxCapturedBytes int64
	workers          int
	queue            chan sessionRecordPayload
	httpClient       *http.Client
	enabled          bool
}

type sessionRecordPayload struct {
	SessionID         string         `json:"session_id"`
	UserID            string         `json:"user_id"`
	RequestID         string         `json:"request_id"`
	CallType          string         `json:"call_type"`
	Provider          string         `json:"provider"`
	Model             string         `json:"model,omitempty"`
	PromptTokens      int64          `json:"prompt_tokens,omitempty"`
	CompletionTokens  int64          `json:"completion_tokens,omitempty"`
	TotalTokens       int64          `json:"total_tokens,omitempty"`
	StartTime         string         `json:"start_time"`
	EndTime           string         `json:"end_time"`
	Status            string         `json:"status"`
	TerminationReason string         `json:"termination_reason,omitempty"`
	UserAgent         string         `json:"user_agent,omitempty"`
	Method            string         `json:"method"`
	Path              string         `json:"path"`
	Query             string         `json:"query,omitempty"`
	Messages          any            `json:"messages,omitempty"`
	Tools             any            `json:"tools,omitempty"`
	Request           any            `json:"request,omitempty"`
	Response          any            `json:"response,omitempty"`
	Source            map[string]any `json:"source"`
	GatewayLog        map[string]any `json:"gateway_log"`
}

type captureResponseWriter struct {
	gin.ResponseWriter
	buffer *bytes.Buffer
	limit  int64
}

func (w *captureResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *captureResponseWriter) WriteString(data string) (int, error) {
	w.capture([]byte(data))
	return w.ResponseWriter.WriteString(data)
}

func (w *captureResponseWriter) capture(data []byte) {
	if w.buffer == nil || w.limit <= 0 {
		return
	}
	remaining := w.limit - int64(w.buffer.Len())
	if remaining <= 0 {
		return
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
	}
	_, _ = w.buffer.Write(data)
}

func newSessionRecorderClient() *sessionRecorderClient {
	endpoint := strings.TrimSpace(common.GetEnvOrDefaultString("SESSION_RECORDER_ENDPOINT", ""))
	if endpoint == "" {
		endpoint = strings.TrimSpace(common.GetEnvOrDefaultString("NEWAPI_SESSION_RECORDER_ENDPOINT", ""))
	}
	endpoint = normalizeSessionRecorderEndpoint(endpoint)
	client := &sessionRecorderClient{
		endpoint:         endpoint,
		nodeName:         firstNonEmpty(strings.TrimSpace(common.NodeName), strings.TrimSpace(common.GetEnvOrDefaultString("NODE_NAME", "")), "new-api"),
		maxCapturedBytes: int64(common.GetEnvOrDefault("SESSION_RECORDER_MAX_CAPTURED_BYTES", defaultSessionRecorderMaxCapturedBytes)),
		workers:          maxSessionRecorderInt(1, common.GetEnvOrDefault("SESSION_RECORDER_WORKERS", defaultSessionRecorderWorkers)),
		queue:            make(chan sessionRecordPayload, common.GetEnvOrDefault("SESSION_RECORDER_QUEUE_SIZE", defaultSessionRecorderQueueSize)),
		httpClient: &http.Client{
			Timeout: time.Duration(common.GetEnvOrDefault("SESSION_RECORDER_TIMEOUT_SECONDS", defaultSessionRecorderRequestTimeoutSec)) * time.Second,
		},
	}
	client.enabled = endpoint != ""
	if client.enabled {
		for i := 0; i < client.workers; i++ {
			go client.worker()
		}
	}
	return client
}

func SessionRecorderCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		client := getSessionRecorderClient()
		if !client.enabled || !shouldSessionRecord(c.Request) {
			c.Next()
			return
		}

		start := time.Now().UTC()
		responseBuffer := &bytes.Buffer{}
		captureWriter := &captureResponseWriter{
			ResponseWriter: c.Writer,
			buffer:         responseBuffer,
			limit:          client.maxCapturedBytes,
		}
		c.Writer = captureWriter

		c.Next()

		payload, ok := buildSessionRecordPayload(c, client, start, responseBuffer.Bytes(), responseBuffer.Len() >= int(client.maxCapturedBytes))
		if ok {
			client.enqueue(payload)
		}
	}
}

func getSessionRecorderClient() *sessionRecorderClient {
	sessionRecorder.once.Do(func() {
		sessionRecorder.client = newSessionRecorderClient()
	})
	return sessionRecorder.client
}

func shouldSessionRecord(req *http.Request) bool {
	if req == nil || req.Method != http.MethodPost {
		return false
	}
	path := req.URL.Path
	switch {
	case path == "/pg/chat/completions":
		return true
	case path == "/v1/chat/completions",
		path == "/v1/completions",
		path == "/v1/edits",
		path == "/v1/responses",
		path == "/v1/responses/compact",
		path == "/v1/messages":
		return true
	case strings.HasPrefix(path, "/v1/images/"):
		return true
	case strings.HasPrefix(path, "/v1beta/models/") && (strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")):
		return true
	case strings.HasPrefix(path, "/v1/models/") && (strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")):
		return true
	default:
		return false
	}
}

func buildSessionRecordPayload(c *gin.Context, client *sessionRecorderClient, start time.Time, responseBody []byte, responseTruncated bool) (sessionRecordPayload, bool) {
	requestObj, requestTruncated := capturedRequest(c, client.maxCapturedBytes)
	provider, endpoint := classifySessionRecorderEndpoint(c.Request.URL.Path)
	model := requestStringField(requestObj, "model")
	requestID := firstNonEmpty(c.GetString(common.RequestIdKey), c.Writer.Header().Get("X-Oneapi-Request-Id"), newSessionRecorderID())
	apiKeyHash := hashSessionRecorderSecret(firstNonEmpty(
		common.GetContextKeyString(c, constant.ContextKeyTokenKey),
		c.GetHeader("Authorization"),
		c.GetHeader("x-api-key"),
		c.Query("key"),
	))
	sessionID := sessionIDFromCapturedRequest(c, requestObj, apiKeyHash)
	userID := fmt.Sprintf("%d", common.GetContextKeyInt(c, constant.ContextKeyUserId))
	if userID == "0" {
		userID = firstNonEmpty(apiKeyHash, "anonymous")
	}

	responseObj := normalizeCapturedResponse(responseBody, c.Writer.Header().Get("Content-Type"))
	statusCode := c.Writer.Status()
	status := "ok"
	if statusCode >= http.StatusBadRequest {
		status = "error"
	}
	promptTokens, completionTokens, totalTokens := usageFromCapturedResponse(responseObj)
	messages := extractSessionRecorderMessages(provider, requestObj)
	tools := extractSessionRecorderTools(requestObj)

	payload := sessionRecordPayload{
		SessionID:        firstNonEmpty(sessionID, requestID),
		UserID:           userID,
		RequestID:        requestID,
		CallType:         "newapi_embedded",
		Provider:         provider,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		StartTime:        start.Format(time.RFC3339Nano),
		EndTime:          time.Now().UTC().Format(time.RFC3339Nano),
		Status:           status,
		UserAgent:        c.Request.UserAgent(),
		Method:           c.Request.Method,
		Path:             c.Request.URL.Path,
		Query:            c.Request.URL.RawQuery,
		Messages:         messages,
		Tools:            tools,
		Request:          requestObj,
		Response:         responseObj,
		Source: map[string]any{
			"type": "newapi-embedded",
			"node": client.nodeName,
		},
		GatewayLog: map[string]any{
			"host":               c.Request.Host,
			"status_code":        statusCode,
			"duration_ms":        time.Since(start).Milliseconds(),
			"endpoint":           endpoint,
			"stream":             common.GetContextKeyBool(c, constant.ContextKeyIsStream),
			"request_truncated":  requestTruncated,
			"response_truncated": responseTruncated,
			"api_key_sha256":     apiKeyHash,
			"channel_id":         common.GetContextKeyInt(c, constant.ContextKeyChannelId),
			"token_id":           common.GetContextKeyInt(c, constant.ContextKeyTokenId),
			"token_name":         c.GetString("token_name"),
			"group":              common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
			"model":              model,
		},
	}
	return payload, true
}

func capturedRequest(c *gin.Context, maxBytes int64) (any, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, gin.MIMEMultipartPOSTForm) {
		return multipartSummary(c), storage.Size() > maxBytes
	}
	if storage.Size() > maxBytes {
		return map[string]any{
			"content_type": contentType,
			"size":         storage.Size(),
			"truncated":    true,
		}, true
	}
	data, err := storage.Bytes()
	if err != nil {
		return nil, false
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		var obj any
		if err := common.Unmarshal(data, &obj); err == nil {
			return obj, false
		}
	}
	return string(data), false
}

func multipartSummary(c *gin.Context) map[string]any {
	result := map[string]any{
		"content_type": "multipart/form-data",
	}
	if c.Request == nil || c.Request.MultipartForm == nil {
		result["note"] = "multipart form not parsed"
		return result
	}
	fields := map[string]any{}
	for key, values := range c.Request.MultipartForm.Value {
		if len(values) == 1 {
			fields[key] = values[0]
		} else {
			fields[key] = values
		}
	}
	files := map[string]any{}
	for key, headers := range c.Request.MultipartForm.File {
		items := make([]map[string]any, 0, len(headers))
		for _, header := range headers {
			items = append(items, map[string]any{
				"filename": header.Filename,
				"size":     header.Size,
			})
		}
		files[key] = items
	}
	result["fields"] = fields
	result["files"] = files
	return result
}

func normalizeCapturedResponse(data []byte, contentType string) any {
	if len(data) == 0 {
		return nil
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		var obj any
		if err := common.Unmarshal(data, &obj); err == nil {
			return obj
		}
	}
	if mediaType == "text/event-stream" {
		return normalizeSSECapture(data)
	}
	return string(data)
}

func normalizeSSECapture(data []byte) any {
	var events []any
	var text strings.Builder
	var current []string
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			appendSSEEvent(current, &events, &text)
			current = current[:0]
			continue
		}
		current = append(current, line)
	}
	appendSSEEvent(current, &events, &text)
	return map[string]any{
		"type":        "sse",
		"events":      events,
		"event_count": len(events),
		"text":        text.String(),
	}
}

func appendSSEEvent(lines []string, events *[]any, text *strings.Builder) {
	var dataParts []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataParts = append(dataParts, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataParts) == 0 {
		return
	}
	data := strings.Join(dataParts, "\n")
	if data == "" || data == "[DONE]" {
		return
	}
	var obj any
	if err := common.Unmarshal([]byte(data), &obj); err != nil {
		obj = data
	}
	appendCapturedText(obj, text)
	*events = append(*events, obj)
}

func appendCapturedText(value any, builder *strings.Builder) {
	switch v := value.(type) {
	case map[string]any:
		if choices, ok := v["choices"].([]any); ok {
			for _, choiceRaw := range choices {
				choice, _ := choiceRaw.(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					appendCapturedString(delta["content"], builder)
				}
				if message, ok := choice["message"].(map[string]any); ok {
					appendCapturedString(message["content"], builder)
				}
			}
		}
		if response, ok := v["response"].(map[string]any); ok {
			appendCapturedText(response, builder)
		}
		for _, key := range []string{"delta", "text", "content", "output_text"} {
			appendCapturedString(v[key], builder)
		}
		if parts, ok := v["parts"].([]any); ok {
			appendCapturedText(parts, builder)
		}
	case []any:
		for _, item := range v {
			appendCapturedText(item, builder)
		}
	}
}

func appendCapturedString(value any, builder *strings.Builder) {
	switch v := value.(type) {
	case string:
		builder.WriteString(v)
	case []any:
		for _, item := range v {
			appendCapturedString(item, builder)
		}
	}
}

func usageFromCapturedResponse(value any) (int64, int64, int64) {
	obj, ok := value.(map[string]any)
	if !ok {
		return 0, 0, 0
	}
	if usage, ok := obj["usage"].(map[string]any); ok {
		prompt := numberField(usage, "prompt_tokens", "input_tokens")
		completion := numberField(usage, "completion_tokens", "output_tokens")
		total := numberField(usage, "total_tokens")
		if total == 0 {
			total = prompt + completion
		}
		return prompt, completion, total
	}
	if response, ok := obj["response"].(map[string]any); ok {
		return usageFromCapturedResponse(response)
	}
	return 0, 0, 0
}

func numberField(obj map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := obj[key].(type) {
		case float64:
			return int64(value)
		case int64:
			return value
		case int:
			return int64(value)
		}
	}
	return 0
}

func requestStringField(value any, key string) string {
	obj, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	text, _ := obj[key].(string)
	return strings.TrimSpace(text)
}

func extractSessionRecorderMessages(provider string, value any) any {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	switch provider {
	case "gemini":
		return firstExistingValue(obj, "contents", "system_instruction", "systemInstruction")
	default:
		return firstExistingValue(obj, "messages", "input", "prompt", "system", "instructions")
	}
}

func extractSessionRecorderTools(value any) any {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return firstExistingValue(obj, "tools", "tool_choice", "functions", "function_call", "toolConfig", "tool_config")
}

func firstExistingValue(obj map[string]any, keys ...string) any {
	result := map[string]any{}
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	if len(result) == 1 {
		for _, value := range result {
			return value
		}
	}
	return result
}

func sessionIDFromCapturedRequest(c *gin.Context, requestObj any, apiKeyHash string) string {
	for _, header := range []string{"X-Session-Id", "X-Conversation-Id", "OpenAI-Conversation-ID"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	obj, ok := requestObj.(map[string]any)
	if ok {
		for _, key := range []string{"session_id", "conversation_id", "prompt_cache_key", "previous_response_id", "response_id", "thread_id"} {
			if value := requestStringField(obj, key); value != "" {
				return value
			}
		}
		for _, key := range []string{"messages", "input", "contents", "prompt"} {
			if value, exists := obj[key]; exists {
				return hashSessionRecorderAny(key, apiKeyHash, value)
			}
		}
	}
	return firstNonEmpty(apiKeyHash, c.ClientIP(), "anonymous")
}

func classifySessionRecorderEndpoint(path string) (string, string) {
	switch {
	case path == "/pg/chat/completions":
		return "openai", "playground_chat_completions"
	case path == "/v1/chat/completions":
		return "openai", "chat_completions"
	case path == "/v1/completions":
		return "openai", "completions"
	case path == "/v1/edits":
		return "openai", "edits"
	case path == "/v1/responses/compact":
		return "openai", "responses_compact"
	case path == "/v1/responses":
		return "openai", "responses"
	case path == "/v1/messages":
		return "claude", "messages"
	case strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent"):
		return "gemini", "generate_content"
	case strings.HasPrefix(path, "/v1/images"):
		return "openai", "images"
	default:
		return "unknown", "unknown"
	}
}

func (client *sessionRecorderClient) enqueue(payload sessionRecordPayload) {
	select {
	case client.queue <- payload:
	default:
		common.SysError("session recorder queue full, dropping record")
	}
}

func (client *sessionRecorderClient) worker() {
	for payload := range client.queue {
		client.post(payload)
	}
}

func (client *sessionRecorderClient) post(payload sessionRecordPayload) {
	data, err := common.Marshal(payload)
	if err != nil {
		common.SysError("session recorder marshal failed: " + err.Error())
		return
	}
	req, err := http.NewRequest(http.MethodPost, client.endpoint, bytes.NewReader(data))
	if err != nil {
		common.SysError("session recorder request build failed: " + err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		common.SysError("session recorder post failed: " + err.Error())
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		common.SysError(fmt.Sprintf("session recorder post returned status %d", resp.StatusCode))
	}
}

func hashSessionRecorderSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hashSessionRecorderAny(parts ...any) string {
	hash := sha256.New()
	for _, part := range parts {
		data, _ := common.Marshal(part)
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:24]
}

func newSessionRecorderID() string {
	return fmt.Sprintf("%x", time.Now().UTC().UnixNano())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeSessionRecorderEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return endpoint
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/record"
	}
	return parsed.String()
}

func maxSessionRecorderInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
