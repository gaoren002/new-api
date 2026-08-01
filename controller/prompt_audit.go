package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

func promptAuditGroup(c *gin.Context, usingGroup string) string {
	if selectedGroup := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); selectedGroup != "" {
		return selectedGroup
	}
	return strings.TrimSpace(usingGroup)
}

type promptAuditPublicEndpoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	TimeoutMS   int    `json:"timeout_ms"`
	InputLimit  int    `json:"input_limit"`
	Enabled     bool   `json:"enabled"`
	HasToken    bool   `json:"has_token"`
	TokenStatus string `json:"token_status"`
}

type promptAuditPublicConfig struct {
	Mode                    setting.PromptAuditMode                   `json:"mode"`
	BlockingLatestTurnOnly  bool                                      `json:"blocking_latest_turn_only"`
	StorePassEvents         bool                                      `json:"store_pass_events"`
	StoreBlockedEventsOnly  bool                                      `json:"store_blocked_events_only"`
	Strategy                string                                    `json:"strategy"`
	WorkerCount             int                                       `json:"worker_count"`
	QueueCapacity           int                                       `json:"queue_capacity"`
	Scanners                []string                                  `json:"scanners"`
	FailClosed              bool                                      `json:"fail_closed"`
	Endpoints               []promptAuditPublicEndpoint               `json:"endpoints"`
	GroupPolicies           map[string]setting.PromptAuditGroupPolicy `json:"group_policies"`
	ConfigVersion           int64                                     `json:"config_version"`
	UpdatedAt               time.Time                                 `json:"updated_at"`
	UpdatedBy               int                                       `json:"updated_by"`
	EncryptionKeyConfigured bool                                      `json:"encryption_key_configured"`
	ContentModerationActive bool                                      `json:"content_moderation_active"`
}

type promptAuditUpdateEndpoint struct {
	ID         string `json:"id" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Protocol   string `json:"protocol"`
	BaseURL    string `json:"base_url" binding:"required"`
	Model      string `json:"model"`
	Token      string `json:"token,omitempty"`
	ClearToken bool   `json:"clear_token"`
	TimeoutMS  int    `json:"timeout_ms"`
	InputLimit int    `json:"input_limit"`
	Enabled    bool   `json:"enabled"`
}

type promptAuditUpdateConfig struct {
	ExpectedConfigVersion  int64                                     `json:"expected_config_version" binding:"required"`
	Mode                   setting.PromptAuditMode                   `json:"mode"`
	BlockingLatestTurnOnly bool                                      `json:"blocking_latest_turn_only"`
	StorePassEvents        bool                                      `json:"store_pass_events"`
	StoreBlockedEventsOnly bool                                      `json:"store_blocked_events_only"`
	Strategy               string                                    `json:"strategy"`
	WorkerCount            int                                       `json:"worker_count"`
	QueueCapacity          int                                       `json:"queue_capacity"`
	Scanners               []string                                  `json:"scanners"`
	FailClosed             bool                                      `json:"fail_closed"`
	Endpoints              []promptAuditUpdateEndpoint               `json:"endpoints"`
	GroupPolicies          map[string]setting.PromptAuditGroupPolicy `json:"group_policies"`
}

func GetPromptAuditConfig(c *gin.Context) {
	storage, err := setting.GetPromptAuditStorageConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "prompt audit config is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": promptAuditPublicConfigFromStorage(storage)})
}

func UpdatePromptAuditConfig(c *gin.Context) {
	var request promptAuditUpdateConfig
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit config"})
		return
	}
	current, err := setting.GetPromptAuditStorageConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "prompt audit config is unavailable"})
		return
	}
	if current.ConfigVersion != request.ExpectedConfigVersion {
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "prompt_audit_config_conflict", "message": "prompt audit config was updated by another administrator"})
		return
	}
	oldByID := make(map[string]setting.PromptAuditEndpoint, len(current.Endpoints))
	for _, endpoint := range current.Endpoints {
		oldByID[endpoint.ID] = endpoint
	}
	next := setting.PromptAuditStorageConfig{
		Mode: request.Mode, BlockingLatestTurnOnly: request.BlockingLatestTurnOnly,
		StorePassEvents: request.StorePassEvents, StoreBlockedEventsOnly: request.StoreBlockedEventsOnly,
		Strategy:    strings.TrimSpace(request.Strategy),
		WorkerCount: request.WorkerCount, QueueCapacity: request.QueueCapacity,
		Scanners: append([]string(nil), request.Scanners...), FailClosed: true,
		GroupPolicies: request.GroupPolicies, ConfigVersion: current.ConfigVersion + 1,
		UpdatedAt: time.Now().UTC(), UpdatedBy: common.GetContextKeyInt(c, constant.ContextKeyUserId),
	}
	for group, policy := range next.GroupPolicies {
		policy.FailClosed = true
		next.GroupPolicies[group] = policy
	}
	for _, input := range request.Endpoints {
		endpoint := setting.PromptAuditEndpoint{
			ID: strings.TrimSpace(input.ID), Name: strings.TrimSpace(input.Name), Protocol: strings.TrimSpace(input.Protocol),
			BaseURL: strings.TrimSpace(input.BaseURL), Model: strings.TrimSpace(input.Model),
			TimeoutMS: input.TimeoutMS, InputLimit: input.InputLimit, Enabled: input.Enabled,
		}
		old, existingEndpoint := oldByID[endpoint.ID]
		switch {
		case input.ClearToken:
		case strings.TrimSpace(input.Token) != "":
			ciphertext, encryptErr := setting.EncryptPromptAuditToken(input.Token)
			if encryptErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "prompt_audit_encryption_key_required", "message": encryptErr.Error()})
				return
			}
			endpoint.TokenCiphertext = ciphertext
		default:
			endpoint.TokenCiphertext = old.TokenCiphertext
			if existingEndpoint && endpoint.TokenCiphertext == "" && strings.TrimSpace(setting.PromptAuditConfigJSON) == "" && strings.TrimSpace(setting.PromptAuditAPIKey) != "" {
				ciphertext, encryptErr := setting.EncryptPromptAuditToken(setting.PromptAuditAPIKey)
				if encryptErr != nil {
					c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "prompt_audit_encryption_key_required", "message": encryptErr.Error()})
					return
				}
				endpoint.TokenCiphertext = ciphertext
			}
		}
		next.Endpoints = append(next.Endpoints, endpoint)
	}
	if err := setting.ValidatePromptAuditStorageConfig(next); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	raw, err := json.Marshal(next)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to encode prompt audit config"})
		return
	}
	if err := model.UpdateSecurityAuditOptionCAS(setting.PromptAuditConfigOptionKey, request.ExpectedConfigVersion, string(raw)); err != nil {
		status := http.StatusInternalServerError
		code := "prompt_audit_config_save_failed"
		message := "failed to save prompt audit config"
		if errors.Is(err, model.ErrOptionVersionConflict) {
			status, code, message = http.StatusConflict, "prompt_audit_config_conflict", "prompt audit config was updated by another administrator"
		} else if errors.Is(err, model.ErrSecurityAuditEngineConflict) {
			status, code, message = http.StatusConflict, "security_audit_engine_conflict", "content moderation and prompt audit cannot be enabled at the same time"
		}
		c.JSON(status, gin.H{"success": false, "code": code, "message": message})
		return
	}
	service.PublishPromptAuditConfigInvalidation(c.Request.Context(), next.ConfigVersion)
	recordManageAudit(c, "prompt_audit.config_update", map[string]interface{}{
		"from_version": current.ConfigVersion, "to_version": next.ConfigVersion,
		"mode": next.Mode, "endpoint_count": len(next.Endpoints), "group_policy_count": len(next.GroupPolicies),
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": promptAuditPublicConfigFromStorage(next)})
}

func promptAuditPublicConfigFromStorage(storage setting.PromptAuditStorageConfig) promptAuditPublicConfig {
	public := promptAuditPublicConfig{
		Mode: storage.Mode, BlockingLatestTurnOnly: storage.BlockingLatestTurnOnly,
		StorePassEvents: storage.StorePassEvents, StoreBlockedEventsOnly: storage.StoreBlockedEventsOnly,
		Strategy:    storage.Strategy,
		WorkerCount: storage.WorkerCount, QueueCapacity: storage.QueueCapacity,
		Scanners: append([]string(nil), storage.Scanners...), FailClosed: storage.FailClosed,
		GroupPolicies: storage.GroupPolicies, ConfigVersion: storage.ConfigVersion,
		UpdatedAt: storage.UpdatedAt, UpdatedBy: storage.UpdatedBy,
		EncryptionKeyConfigured: setting.PromptAuditEncryptionKeyConfigured(),
		ContentModerationActive: setting.ContentModerationJSONActive(setting.ContentModerationConfigJSON),
	}
	for _, endpoint := range storage.Endpoints {
		status := "missing"
		hasToken := endpoint.TokenCiphertext != ""
		if !hasToken && strings.TrimSpace(setting.PromptAuditConfigJSON) == "" && strings.TrimSpace(setting.PromptAuditAPIKey) != "" {
			hasToken, status = true, "configured"
		}
		if endpoint.TokenCiphertext != "" {
			status = "configured"
			if _, err := setting.DecryptPromptAuditToken(endpoint.TokenCiphertext); err != nil {
				status = "invalid"
			}
		}
		public.Endpoints = append(public.Endpoints, promptAuditPublicEndpoint{
			ID: endpoint.ID, Name: endpoint.Name, Protocol: endpoint.Protocol, BaseURL: endpoint.BaseURL,
			Model: endpoint.Model, TimeoutMS: endpoint.TimeoutMS, InputLimit: endpoint.InputLimit,
			Enabled: endpoint.Enabled, HasToken: hasToken, TokenStatus: status,
		})
	}
	return public
}

func ProbePromptAuditNode(c *gin.Context) {
	var request promptAuditUpdateEndpoint
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit endpoint"})
		return
	}
	storage, _ := setting.GetPromptAuditStorageConfig()
	endpoint := setting.PromptAuditEndpoint{
		ID: strings.TrimSpace(request.ID), Name: strings.TrimSpace(request.Name), Protocol: strings.TrimSpace(request.Protocol),
		BaseURL: strings.TrimSpace(request.BaseURL), Model: strings.TrimSpace(request.Model),
		TimeoutMS: request.TimeoutMS, InputLimit: request.InputLimit, Enabled: true,
	}
	if endpoint.Protocol == "" {
		endpoint.Protocol = "openai_compatible"
	}
	probeToken := request.Token
	if probeToken == "" {
		for _, stored := range storage.Endpoints {
			if stored.ID == endpoint.ID && stored.BaseURL == endpoint.BaseURL {
				endpoint.TokenCiphertext = stored.TokenCiphertext
				if endpoint.TokenCiphertext == "" && strings.TrimSpace(setting.PromptAuditConfigJSON) == "" {
					probeToken = setting.PromptAuditAPIKey
				}
				break
			}
		}
	}
	result := service.ProbePromptAuditEndpoint(c.Request.Context(), endpoint, probeToken, storage.Scanners)
	recordManageAudit(c, "prompt_audit.endpoint_probe", map[string]interface{}{
		"endpoint_id": endpoint.ID, "status": result.Status, "latency_ms": result.LatencyMS,
		"http_status": result.HTTPStatus, "error_code": result.ErrorCode,
	})
	c.JSON(http.StatusOK, gin.H{"success": result.OK, "data": result, "message": result.Message})
}

func GetPromptAuditRuntime(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": service.GetPromptAuditRuntime()})
}

func ListPromptAuditEvents(c *gin.Context) {
	page := promptAuditQueryInt(c, "page", 1)
	pageSize := promptAuditQueryInt(c, "page_size", 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := promptAuditEventFilter(c)
	events, total, err := model.ListPromptAuditEvents(filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to list prompt audit events"})
		return
	}
	items := make([]map[string]any, 0, len(events))
	for _, event := range events {
		items = append(items, service.DecodePromptAuditEvent(event))
	}
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages}})
}

func GetPromptAuditEvent(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	event, err := model.GetPromptAuditEvent(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "prompt audit event not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": service.DecodePromptAuditEvent(*event)})
}

func DeletePromptAuditEvent(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit event ID"})
		return
	}
	deleted, err := model.DeletePromptAuditEvents([]int64{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to delete prompt audit event"})
		return
	}
	service.DeletePromptAuditJobPayloads(c.Request.Context(), deleted.JobIDs)
	recordManageAudit(c, "prompt_audit.event_delete", map[string]interface{}{
		"event_id": id, "deleted_events": deleted.DeletedEvents, "deleted_jobs": deleted.DeletedJobs,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": deleted})
}

func DeletePromptAuditEvents(c *gin.Context) {
	var request struct {
		IDs []int64 `json:"ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.IDs) == 0 || len(request.IDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "event IDs are required"})
		return
	}
	for _, id := range request.IDs {
		if id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit event ID"})
			return
		}
	}
	deleted, err := model.DeletePromptAuditEvents(request.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to delete prompt audit events"})
		return
	}
	service.DeletePromptAuditJobPayloads(c.Request.Context(), deleted.JobIDs)
	recordManageAudit(c, "prompt_audit.event_delete_batch", map[string]interface{}{
		"requested_count": len(request.IDs), "deleted_events": deleted.DeletedEvents, "deleted_jobs": deleted.DeletedJobs,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": deleted})
}

func PreviewPromptAuditEventDelete(c *gin.Context) {
	filter := promptAuditEventFilter(c)
	preview, err := model.PromptAuditDeletePreviewForFilter(filter)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	token, expiresAt, err := service.NewPromptAuditDeleteConfirmation(common.GetContextKeyInt(c, constant.ContextKeyUserId), preview.FilterHash, preview.SnapshotMaxID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create prompt audit deletion confirmation"})
		return
	}
	recordManageAudit(c, "prompt_audit.event_delete_preview", map[string]interface{}{
		"matched_count": preview.MatchedCount, "snapshot_max_id": preview.SnapshotMaxID,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"matched_count": preview.MatchedCount, "filter_summary": preview.FilterSummary,
		"snapshot_max_id": preview.SnapshotMaxID, "filter_hash": preview.FilterHash,
		"confirmation_token": token, "expires_at": expiresAt,
	}})
}

func DeletePromptAuditEventsByFilter(c *gin.Context) {
	var request struct {
		Filter            model.PromptAuditEventFilter `json:"filter"`
		SnapshotMaxID     int64                        `json:"snapshot_max_id"`
		FilterHash        string                       `json:"filter_hash"`
		ConfirmationToken string                       `json:"confirmation_token"`
		Confirm           bool                         `json:"confirm"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || !request.Confirm || request.SnapshotMaxID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "confirmed deletion snapshot is required"})
		return
	}
	filterHash := model.PromptAuditFilterHash(request.Filter, request.SnapshotMaxID)
	if !strings.EqualFold(filterHash, request.FilterHash) || service.ValidatePromptAuditDeleteConfirmation(
		request.ConfirmationToken, common.GetContextKeyInt(c, constant.ContextKeyUserId), filterHash, request.SnapshotMaxID,
	) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "deletion confirmation is invalid or expired"})
		return
	}
	deleted, err := model.DeletePromptAuditEventsByFilter(request.Filter, request.SnapshotMaxID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "failed to delete prompt audit events"})
		return
	}
	service.DeletePromptAuditJobPayloads(c.Request.Context(), deleted.JobIDs)
	recordManageAudit(c, "prompt_audit.event_delete_filter", map[string]interface{}{
		"deleted_events": deleted.DeletedEvents, "deleted_jobs": deleted.DeletedJobs, "snapshot_max_id": request.SnapshotMaxID,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": deleted})
}

func promptAuditEventFilter(c *gin.Context) model.PromptAuditEventFilter {
	return model.PromptAuditEventFilter{
		Decision: strings.TrimSpace(c.Query("decision")), RiskLevel: strings.TrimSpace(c.Query("risk_level")),
		Group: strings.TrimSpace(c.Query("group")), UserId: promptAuditQueryInt(c, "user_id", 0),
		TokenId: promptAuditQueryInt(c, "token_id", 0), Model: strings.TrimSpace(c.Query("model")),
		RequestId: strings.TrimSpace(c.Query("request_id")), PromptHash: strings.TrimSpace(c.Query("prompt_hash")),
		Endpoint: strings.TrimSpace(c.Query("endpoint")), Keyword: strings.TrimSpace(c.Query("keyword")),
		StartTime: int64(promptAuditQueryInt(c, "start_time", 0)), EndTime: int64(promptAuditQueryInt(c, "end_time", 0)),
	}
}

func promptAuditQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}

func evaluatePromptAudit(c *gin.Context, relayFormat types.RelayFormat, group string) *types.NewAPIError {
	return evaluatePromptAuditWithRelayInfo(c, relayFormat, group, nil)
}

func evaluateSecurityAuditWithRelayInfo(c *gin.Context, relayFormat types.RelayFormat, group string, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	contentConfig, err := setting.GetContentModerationStorageConfig()
	if err == nil && setting.ContentModerationStorageActive(contentConfig) {
		return evaluateContentModerationWithRelayInfo(c, relayFormat, group, relayInfo)
	}
	return evaluatePromptAuditWithRelayInfo(c, relayFormat, group, relayInfo)
}

func evaluateSecurityAuditPayload(c *gin.Context, relayFormat types.RelayFormat, group string, relayInfo *relaycommon.RelayInfo, body []byte, stage string) *types.NewAPIError {
	contentConfig, err := setting.GetContentModerationStorageConfig()
	if err == nil && setting.ContentModerationStorageActive(contentConfig) {
		return evaluateContentModerationPayload(c, relayFormat, group, relayInfo, body)
	}
	return evaluatePromptAuditPayload(c, relayFormat, group, relayInfo, body, stage)
}

func evaluateContentModerationWithRelayInfo(c *gin.Context, relayFormat types.RelayFormat, group string, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	bodyStorage, err := common.GetBodyStorage(c)
	if err != nil {
		status := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, status, types.ErrOptionWithSkipRetry())
	}
	body, err := bodyStorage.Bytes()
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	body, err = contentModerationJSONBody(c, body)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if len(body) == 0 {
		return nil
	}
	return evaluateContentModerationPayload(c, relayFormat, group, relayInfo, body)
}

func evaluateContentModerationPayload(c *gin.Context, relayFormat types.RelayFormat, group string, relayInfo *relaycommon.RelayInfo, body []byte) *types.NewAPIError {
	protocol, supported := promptAuditProtocol(relayFormat)
	if !supported || !json.Valid(body) {
		return nil
	}
	input := service.ContentModerationCheckInput{
		RequestID: c.GetString(common.RequestIdKey), UserID: common.GetContextKeyInt(c, constant.ContextKeyUserId),
		Username: common.GetContextKeyString(c, constant.ContextKeyUserName), UserEmail: common.GetContextKeyString(c, constant.ContextKeyUserEmail),
		TokenID: common.GetContextKeyInt(c, constant.ContextKeyTokenId), TokenName: c.GetString("token_name"),
		Group: group, Endpoint: c.Request.URL.Path, Provider: c.GetString("channel_name"), Protocol: protocol,
		Body: append([]byte(nil), body...),
	}
	if relayInfo != nil {
		input.Model = relayInfo.OriginModelName
	}
	decision, err := service.CheckContentModeration(c.Request.Context(), input)
	if err != nil {
		logger.LogWarn(c, "content moderation configuration is unavailable; request allowed")
		return nil
	}
	if decision != nil && decision.Blocked {
		logger.LogWarn(c, "content moderation blocked request")
		status := decision.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusForbidden
		}
		return types.NewErrorWithStatusCode(errors.New(decision.Message), types.ErrorCodeContentModerationBlocked, status, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func evaluatePromptAuditWithRelayInfo(c *gin.Context, relayFormat types.RelayFormat, group string, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	bodyStorage, err := common.GetBodyStorage(c)
	if err != nil {
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	body, err := bodyStorage.Bytes()
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	body, err = promptAuditJSONBody(c, body)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if len(body) == 0 {
		return nil
	}
	return evaluatePromptAuditPayload(c, relayFormat, group, relayInfo, body, "http")
}

func evaluatePromptAuditPayload(c *gin.Context, relayFormat types.RelayFormat, group string, relayInfo *relaycommon.RelayInfo, body []byte, stage string) *types.NewAPIError {
	config := setting.GetPromptAuditConfigForGroup(group)
	if !config.Enabled {
		return nil
	}
	protocol, supported := promptAuditProtocol(relayFormat)
	if !supported {
		return nil
	}

	if !json.Valid(body) {
		return types.NewErrorWithStatusCode(errors.New("prompt audit request JSON is invalid"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	auditRequest := service.PromptAuditRequest{
		RequestID: c.GetString(common.RequestIdKey),
		UserID:    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		Username:  common.GetContextKeyString(c, constant.ContextKeyUserName),
		UserEmail: common.GetContextKeyString(c, constant.ContextKeyUserEmail),
		TokenID:   common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		TokenName: c.GetString("token_name"), Group: group,
		Endpoint: c.Request.URL.Path, Protocol: protocol, Body: append([]byte(nil), body...), Stage: stage,
	}
	if relayInfo != nil {
		auditRequest.Model = relayInfo.OriginModelName
	}
	if config.Mode == setting.PromptAuditModeAsync {
		service.EnqueuePromptAuditRequest(c.Request.Context(), config, auditRequest)
		return nil
	}
	decision, err := service.EvaluatePromptAuditRequest(c.Request.Context(), config, auditRequest)
	if err != nil {
		logger.LogWarn(c, "prompt audit backend unavailable")
		return types.NewErrorWithStatusCode(errors.New("prompt audit is temporarily unavailable"), types.ErrorCodePromptAuditUnavailable, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if decision != nil && decision.Blocked {
		logger.LogWarn(c, "prompt audit blocked request")
		return types.NewErrorWithStatusCode(errors.New("request blocked by prompt audit"), types.ErrorCodePromptBlocked, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func promptAuditJSONBody(c *gin.Context, body []byte) ([]byte, error) {
	if json.Valid(body) {
		return body, nil
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	values := map[string]any{}
	switch {
	case strings.Contains(contentType, gin.MIMEMultipartPOSTForm):
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		defer form.RemoveAll()
		for key, items := range form.Value {
			if len(items) == 1 {
				values[key] = items[0]
			} else if len(items) > 1 {
				values[key] = items
			}
		}
	case strings.Contains(contentType, gin.MIMEPOSTForm):
		parsed, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		for key, items := range parsed {
			if len(items) == 1 {
				values[key] = items[0]
			} else if len(items) > 1 {
				values[key] = items
			}
		}
	default:
		return nil, nil
	}
	if len(values) == 0 {
		return nil, nil
	}
	return json.Marshal(values)
}

func promptAuditProtocol(relayFormat types.RelayFormat) (string, bool) {
	switch relayFormat {
	case types.RelayFormatOpenAI:
		return "openai_chat_completions", true
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return "openai_responses", true
	case types.RelayFormatClaude:
		return "anthropic_messages", true
	case types.RelayFormatGemini:
		return "gemini", true
	case types.RelayFormatOpenAIImage:
		return "openai_images", true
	case types.RelayFormatOpenAIAlphaSearch:
		return "openai_alpha_search", true
	case types.RelayFormatOpenAIRealtime:
		return "openai_realtime", true
	case types.RelayFormatEmbedding:
		return "openai_embeddings", true
	case types.RelayFormatOpenAIAudio:
		return "openai_audio", true
	case types.RelayFormatRerank:
		return "rerank", true
	case types.RelayFormatTask:
		return "media", true
	case types.RelayFormatMjProxy:
		return "media", true
	default:
		return "", false
	}
}

type promptAuditProbeRequest struct {
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key"`
	TimeoutMS      int    `json:"timeout_ms"`
	InputLimit     int    `json:"input_limit"`
	MaxConcurrency int    `json:"max_concurrency"`
	Scanners       string `json:"scanners"`
}

func TestPromptAudit(c *gin.Context) {
	var request promptAuditProbeRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid prompt audit probe request"})
		return
	}
	config := setting.GetPromptAuditConfig()
	config.Enabled = true
	config.BaseURL = strings.TrimSpace(request.BaseURL)
	config.Model = strings.TrimSpace(request.Model)
	if request.APIKey != "" {
		config.APIKey = strings.TrimSpace(request.APIKey)
	}
	config.TimeoutMS = request.TimeoutMS
	config.InputLimit = request.InputLimit
	config.MaxConcurrency = request.MaxConcurrency
	scanners, err := setting.ParsePromptAuditScanners(request.Scanners)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	config.Scanners = scanners
	started := time.Now()
	if err := service.ProbePromptAudit(c.Request.Context(), config); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "prompt audit backend probe failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "prompt audit backend is available",
		"latency_ms": time.Since(started).Milliseconds(),
	})
}
