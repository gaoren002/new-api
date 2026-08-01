package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromptAuditProtocol(t *testing.T) {
	tests := []struct {
		format    types.RelayFormat
		protocol  string
		supported bool
	}{
		{types.RelayFormatOpenAI, "openai_chat_completions", true},
		{types.RelayFormatOpenAIResponses, "openai_responses", true},
		{types.RelayFormatOpenAIResponsesCompaction, "openai_responses", true},
		{types.RelayFormatClaude, "anthropic_messages", true},
		{types.RelayFormatGemini, "gemini", true},
		{types.RelayFormatOpenAIImage, "openai_images", true},
		{types.RelayFormatOpenAIAlphaSearch, "openai_alpha_search", true},
		{types.RelayFormatOpenAIAudio, "openai_audio", true},
		{types.RelayFormatEmbedding, "openai_embeddings", true},
		{types.RelayFormatOpenAIRealtime, "openai_realtime", true},
		{types.RelayFormatRerank, "rerank", true},
		{types.RelayFormatTask, "media", true},
		{types.RelayFormatMjProxy, "media", true},
	}
	for _, test := range tests {
		protocol, supported := promptAuditProtocol(test.format)
		require.Equal(t, test.protocol, protocol)
		require.Equal(t, test.supported, supported)
	}
}

func TestPromptAuditGroupUsesSelectedAutoGroup(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Equal(t, "codex-pro", promptAuditGroup(context, "codex-pro"))
	common.SetContextKey(context, constant.ContextKeyAutoGroup, "codex-plus")
	require.Equal(t, "codex-plus", promptAuditGroup(context, "auto"))
}

func TestPromptAuditJSONBodyExtractsMultipartTextFieldsOnly(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "draw a safe landscape"))
	file, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("binary-image-canary"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	context.Request.Header.Set("Content-Type", writer.FormDataContentType())
	storage, err := common.CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)

	auditBody, err := promptAuditJSONBody(context, body.Bytes())
	require.NoError(t, err)
	require.JSONEq(t, `{"prompt":"draw a safe landscape"}`, string(auditBody))
	require.NotContains(t, string(auditBody), "binary-image-canary")
}

func TestEvaluatePromptAuditBlocksBeforeRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: Jailbreak"}}]}`))
	}))
	defer server.Close()

	previous := setting.GetPromptAuditConfig()
	previousPolicies := setting.PromptAuditGroupPolicies
	defer restorePromptAuditConfig(previous, previousPolicies)
	setPromptAuditTestConfig(server.URL, true)
	context := promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	newAPIError := evaluatePromptAudit(context, types.RelayFormatOpenAI, "default")
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodePromptBlocked, newAPIError.GetErrorCode())
	require.Equal(t, http.StatusForbidden, newAPIError.StatusCode)
}

func TestEvaluatePromptAuditAlwaysFailsClosed(t *testing.T) {
	previous := setting.GetPromptAuditConfig()
	previousPolicies := setting.PromptAuditGroupPolicies
	defer restorePromptAuditConfig(previous, previousPolicies)
	setPromptAuditTestConfig("http://127.0.0.1:1", true)
	context := promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	newAPIError := evaluatePromptAudit(context, types.RelayFormatOpenAI, "default")
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodePromptAuditUnavailable, newAPIError.GetErrorCode())
	require.Equal(t, http.StatusServiceUnavailable, newAPIError.StatusCode)

	setting.PromptAuditFailClosed = false
	context = promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	newAPIError = evaluatePromptAudit(context, types.RelayFormatOpenAI, "default")
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodePromptAuditUnavailable, newAPIError.GetErrorCode())
}

func TestEvaluatePromptAuditUsesGroupPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: PII"}}]}`))
	}))
	defer server.Close()

	previous := setting.GetPromptAuditConfig()
	previousPolicies := setting.PromptAuditGroupPolicies
	defer restorePromptAuditConfig(previous, previousPolicies)
	setPromptAuditTestConfig(server.URL, false)
	setting.PromptAuditScanners = "jailbreak"
	setting.PromptAuditGroupPolicies = `{"codex-pro":{"enabled":true,"fail_closed":true,"scanners":["pii"]},"free":{"enabled":false,"fail_closed":false,"scanners":["pii"]}}`

	context := promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	require.Nil(t, evaluatePromptAudit(context, types.RelayFormatOpenAI, "default"))
	context = promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	newAPIError := evaluatePromptAudit(context, types.RelayFormatOpenAI, "codex-pro")
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodePromptBlocked, newAPIError.GetErrorCode())
	context = promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	require.Nil(t, evaluatePromptAudit(context, types.RelayFormatOpenAI, "free"))
}

func TestEvaluatePromptAuditUsesGroupPolicyWhenDefaultModeOff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: PII"}}]}`))
	}))
	defer server.Close()

	previous := setting.GetPromptAuditConfig()
	previousPolicies := setting.PromptAuditGroupPolicies
	previousConfigJSON := setting.PromptAuditConfigJSON
	defer func() {
		setting.PromptAuditConfigJSON = previousConfigJSON
		restorePromptAuditConfig(previous, previousPolicies)
	}()

	storage := setting.DefaultPromptAuditStorageConfig()
	storage.Mode = setting.PromptAuditModeOff
	storage.Endpoints[0].BaseURL = server.URL
	storage.Endpoints[0].Model = "guard-model"
	storage.Endpoints[0].TimeoutMS = 1000
	storage.Endpoints[0].InputLimit = 8000
	storage.GroupPolicies = map[string]setting.PromptAuditGroupPolicy{
		"codex-pro": {
			Mode: setting.PromptAuditModeBlocking, Enabled: true, FailClosed: true, Scanners: []string{"pii"},
		},
	}
	raw, err := json.Marshal(storage)
	require.NoError(t, err)
	setting.PromptAuditConfigJSON = string(raw)

	context := promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	require.Nil(t, evaluatePromptAudit(context, types.RelayFormatOpenAI, "default"))
	context = promptAuditGinContext(t, `{"messages":[{"role":"user","content":"latest input"}]}`)
	newAPIError := evaluatePromptAudit(context, types.RelayFormatOpenAI, "codex-pro")
	require.NotNil(t, newAPIError)
	require.Equal(t, types.ErrorCodePromptBlocked, newAPIError.GetErrorCode())
}

func TestPromptAuditProbeUsesStoredKeyWithoutExposingIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer stored-secret", request.Header.Get("Authorization"))
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer server.Close()

	previous := setting.GetPromptAuditConfig()
	previousPolicies := setting.PromptAuditGroupPolicies
	defer restorePromptAuditConfig(previous, previousPolicies)
	setPromptAuditTestConfig(server.URL, false)
	setting.PromptAuditAPIKey = "stored-secret"

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/option/prompt_audit/test", strings.NewReader(`{
		"base_url":`+mustJSONController(t, server.URL)+`,
		"model":"guard-model",
		"api_key":"",
		"timeout_ms":1000,
		"input_limit":8000,
		"max_concurrency":4,
		"scanners":"jailbreak,pii"
	}`))
	TestPromptAudit(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotContains(t, recorder.Body.String(), "stored-secret")
}

func TestGetOptionsOmitsPromptAuditAPIKey(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{
		"PromptAuditAPIKey":                "stored-secret",
		"PromptAuditKeyConfigured":         "true",
		setting.PromptAuditConfigOptionKey: `{"endpoints":[{"token_ciphertext":"secret-canary"}]}`,
	}
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	}()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	GetOptions(context)
	var response struct {
		Data []struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	keys := make([]string, 0, len(response.Data))
	for _, option := range response.Data {
		keys = append(keys, option.Key)
	}
	require.NotContains(t, keys, "PromptAuditAPIKey")
	require.NotContains(t, keys, setting.PromptAuditConfigOptionKey)
	require.Contains(t, keys, "PromptAuditKeyConfigured")
	require.NotContains(t, recorder.Body.String(), "stored-secret")
	require.NotContains(t, recorder.Body.String(), "secret-canary")
}

func TestUpdatePromptAuditConfigClearsStoredToken(t *testing.T) {
	usePromptAuditControllerTestDB(t)
	t.Setenv(setting.PromptAuditEncryptionKeyEnv, strings.Repeat("42", 32))
	previousConfig := setting.PromptAuditConfigJSON
	t.Cleanup(func() { setting.PromptAuditConfigJSON = previousConfig })

	storage := setting.DefaultPromptAuditStorageConfig()
	storage.ConfigVersion = 3
	ciphertext, err := setting.EncryptPromptAuditToken("stored-token-canary")
	require.NoError(t, err)
	storage.Endpoints[0].TokenCiphertext = ciphertext
	raw, err := json.Marshal(storage)
	require.NoError(t, err)
	setting.PromptAuditConfigJSON = string(raw)
	require.NoError(t, model.DB.Create(&model.Option{Key: setting.PromptAuditConfigOptionKey, Value: string(raw)}).Error)

	requestBody := promptAuditConfigUpdateBody(t, storage, true)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/prompt-audit/config", strings.NewReader(string(requestBody)))
	common.SetContextKey(context, constant.ContextKeyUserId, 7)
	UpdatePromptAuditConfig(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "stored-token-canary")
	require.NotContains(t, recorder.Body.String(), ciphertext)

	updated, err := setting.GetPromptAuditStorageConfig()
	require.NoError(t, err)
	require.Equal(t, int64(4), updated.ConfigVersion)
	require.Equal(t, 7, updated.UpdatedBy)
	require.Empty(t, updated.Endpoints[0].TokenCiphertext)
}

func TestUpdatePromptAuditConfigMigratesLegacyToken(t *testing.T) {
	usePromptAuditControllerTestDB(t)
	t.Setenv(setting.PromptAuditEncryptionKeyEnv, strings.Repeat("42", 32))
	previousConfig, previousKey := setting.PromptAuditConfigJSON, setting.PromptAuditAPIKey
	t.Cleanup(func() {
		setting.PromptAuditConfigJSON, setting.PromptAuditAPIKey = previousConfig, previousKey
	})
	setting.PromptAuditConfigJSON = ""
	setting.PromptAuditAPIKey = "legacy-token-canary"
	storage := setting.DefaultPromptAuditStorageConfig()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/prompt-audit/config", strings.NewReader(string(promptAuditConfigUpdateBody(t, storage, false))))
	common.SetContextKey(context, constant.ContextKeyUserId, 7)
	UpdatePromptAuditConfig(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "legacy-token-canary")

	updated, err := setting.GetPromptAuditStorageConfig()
	require.NoError(t, err)
	require.NotEmpty(t, updated.Endpoints[0].TokenCiphertext)
	plaintext, err := setting.DecryptPromptAuditToken(updated.Endpoints[0].TokenCiphertext)
	require.NoError(t, err)
	require.Equal(t, "legacy-token-canary", plaintext)
}

func TestPromptAuditFilterDeletionRequiresAdminBoundConfirmation(t *testing.T) {
	usePromptAuditControllerTestDB(t)
	now := time.Now().Unix()
	job := model.PromptAuditJob{Status: model.PromptAuditJobDone, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, model.DB.Create(&job).Error)
	event := model.PromptAuditEvent{CreatedAt: now, JobId: &job.Id, Decision: "critical", RiskLevel: "critical", GroupName: "strict"}
	require.NoError(t, model.DB.Create(&event).Error)

	previewRecorder := httptest.NewRecorder()
	previewContext, _ := gin.CreateTestContext(previewRecorder)
	previewContext.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prompt-audit/events/delete-preview?start_time=%d&end_time=%d&group=strict", now-60, now+60), nil)
	common.SetContextKey(previewContext, constant.ContextKeyUserId, 7)
	PreviewPromptAuditEventDelete(previewContext)
	require.Equal(t, http.StatusOK, previewRecorder.Code)
	var previewResponse struct {
		Data struct {
			MatchedCount      int64                        `json:"matched_count"`
			FilterSummary     model.PromptAuditEventFilter `json:"filter_summary"`
			SnapshotMaxID     int64                        `json:"snapshot_max_id"`
			FilterHash        string                       `json:"filter_hash"`
			ConfirmationToken string                       `json:"confirmation_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(previewRecorder.Body.Bytes(), &previewResponse))
	require.Equal(t, int64(1), previewResponse.Data.MatchedCount)

	deleteBody, err := json.Marshal(map[string]any{
		"filter": previewResponse.Data.FilterSummary, "snapshot_max_id": previewResponse.Data.SnapshotMaxID,
		"filter_hash": previewResponse.Data.FilterHash, "confirmation_token": previewResponse.Data.ConfirmationToken,
		"confirm": true,
	})
	require.NoError(t, err)
	invalidRecorder := httptest.NewRecorder()
	invalidContext, _ := gin.CreateTestContext(invalidRecorder)
	invalidContext.Request = httptest.NewRequest(http.MethodPost, "/api/prompt-audit/events/filter-delete", strings.NewReader(string(deleteBody)))
	common.SetContextKey(invalidContext, constant.ContextKeyUserId, 8)
	DeletePromptAuditEventsByFilter(invalidContext)
	require.Equal(t, http.StatusBadRequest, invalidRecorder.Code)

	deleteRecorder := httptest.NewRecorder()
	deleteContext, _ := gin.CreateTestContext(deleteRecorder)
	deleteContext.Request = httptest.NewRequest(http.MethodPost, "/api/prompt-audit/events/filter-delete", strings.NewReader(string(deleteBody)))
	common.SetContextKey(deleteContext, constant.ContextKeyUserId, 7)
	DeletePromptAuditEventsByFilter(deleteContext)
	require.Equal(t, http.StatusOK, deleteRecorder.Code)
	var eventCount int64
	require.NoError(t, model.DB.Model(&model.PromptAuditEvent{}).Count(&eventCount).Error)
	require.Zero(t, eventCount)
}

func usePromptAuditControllerTestDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:prompt-audit-controller-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.PromptAuditQueueLock{}, &model.PromptAuditJob{}, &model.PromptAuditEvent{}, &model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
}

func promptAuditConfigUpdateBody(t *testing.T, storage setting.PromptAuditStorageConfig, clearToken bool) []byte {
	t.Helper()
	endpoints := make([]map[string]any, 0, len(storage.Endpoints))
	for _, endpoint := range storage.Endpoints {
		endpoints = append(endpoints, map[string]any{
			"id": endpoint.ID, "name": endpoint.Name,
			"protocol": endpoint.Protocol, "base_url": endpoint.BaseURL,
			"model": endpoint.Model, "timeout_ms": endpoint.TimeoutMS,
			"input_limit": endpoint.InputLimit, "enabled": endpoint.Enabled,
			"clear_token": clearToken,
		})
	}
	raw, err := json.Marshal(map[string]any{
		"expected_config_version":   storage.ConfigVersion,
		"mode":                      storage.Mode,
		"blocking_latest_turn_only": storage.BlockingLatestTurnOnly,
		"store_pass_events":         storage.StorePassEvents,
		"strategy":                  storage.Strategy,
		"worker_count":              storage.WorkerCount,
		"queue_capacity":            storage.QueueCapacity,
		"scanners":                  storage.Scanners,
		"fail_closed":               storage.FailClosed,
		"group_policies":            storage.GroupPolicies,
		"endpoints":                 endpoints,
	})
	require.NoError(t, err)
	return raw
}

func promptAuditGinContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	storage, err := common.CreateBodyStorage([]byte(body))
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	context.Set(common.KeyBodyStorage, storage)
	return context
}

func setPromptAuditTestConfig(baseURL string, failClosed bool) {
	setting.PromptAuditEnabled = true
	setting.PromptAuditBaseURL = baseURL
	setting.PromptAuditModel = "guard-model"
	setting.PromptAuditAPIKey = ""
	setting.PromptAuditTimeoutMS = 1000
	setting.PromptAuditFailClosed = failClosed
	setting.PromptAuditInputLimit = 8000
	setting.PromptAuditMaxConcurrency = 4
	setting.PromptAuditScanners = "jailbreak,pii"
	setting.PromptAuditGroupPolicies = "{}"
}

func restorePromptAuditConfig(config setting.PromptAuditConfig, groupPolicies string) {
	setting.PromptAuditEnabled = config.Enabled
	setting.PromptAuditBaseURL = config.BaseURL
	setting.PromptAuditModel = config.Model
	setting.PromptAuditAPIKey = config.APIKey
	setting.PromptAuditTimeoutMS = config.TimeoutMS
	setting.PromptAuditFailClosed = config.FailClosed
	setting.PromptAuditInputLimit = config.InputLimit
	setting.PromptAuditMaxConcurrency = config.MaxConcurrency
	setting.PromptAuditScanners = strings.Join(config.Scanners, ",")
	setting.PromptAuditGroupPolicies = groupPolicies
}

func mustJSONController(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
