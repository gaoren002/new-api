package controller

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

type contentModerationPublicConfig struct {
	Enabled                        bool                                    `json:"enabled"`
	Mode                           string                                  `json:"mode"`
	BaseURL                        string                                  `json:"base_url"`
	Model                          string                                  `json:"model"`
	ProxyURL                       string                                  `json:"proxy_url"`
	APIKeyCount                    int                                     `json:"api_key_count"`
	APIKeyStatuses                 []service.ContentModerationAPIKeyStatus `json:"api_key_statuses"`
	TimeoutMS                      int                                     `json:"timeout_ms"`
	SampleRate                     int                                     `json:"sample_rate"`
	AllGroups                      bool                                    `json:"all_groups"`
	Groups                         []string                                `json:"groups"`
	RecordNonHits                  bool                                    `json:"record_non_hits"`
	Thresholds                     map[string]float64                      `json:"thresholds"`
	WorkerCount                    int                                     `json:"worker_count"`
	QueueSize                      int                                     `json:"queue_size"`
	BlockStatus                    int                                     `json:"block_status"`
	BlockMessage                   string                                  `json:"block_message"`
	EmailOnHit                     bool                                    `json:"email_on_hit"`
	AutoBanEnabled                 bool                                    `json:"auto_ban_enabled"`
	BanThreshold                   int                                     `json:"ban_threshold"`
	ViolationWindowHours           int                                     `json:"violation_window_hours"`
	RetryCount                     int                                     `json:"retry_count"`
	HitRetentionDays               int                                     `json:"hit_retention_days"`
	NonHitRetentionDays            int                                     `json:"non_hit_retention_days"`
	PreHashCheckEnabled            bool                                    `json:"pre_hash_check_enabled"`
	BlockedKeywords                []string                                `json:"blocked_keywords"`
	KeywordBlockingMode            string                                  `json:"keyword_blocking_mode"`
	ModelFilter                    setting.ContentModerationModelFilter    `json:"model_filter"`
	CyberPolicyExcludeFromBanCount bool                                    `json:"cyber_policy_exclude_from_ban_count"`
	CyberSessionBlockEnabled       bool                                    `json:"cyber_session_block_enabled"`
	CyberSessionBlockTTLSeconds    int                                     `json:"cyber_session_block_ttl_seconds"`
	ConfigVersion                  int64                                   `json:"config_version"`
	UpdatedAt                      time.Time                               `json:"updated_at"`
	UpdatedBy                      int                                     `json:"updated_by"`
	EncryptionKeyConfigured        bool                                    `json:"encryption_key_configured"`
	PromptAuditActive              bool                                    `json:"prompt_audit_active"`
}

type contentModerationUpdateConfig struct {
	ExpectedConfigVersion          int64                                `json:"expected_config_version" binding:"required"`
	Enabled                        bool                                 `json:"enabled"`
	Mode                           string                               `json:"mode"`
	BaseURL                        string                               `json:"base_url"`
	Model                          string                               `json:"model"`
	ProxyURL                       string                               `json:"proxy_url"`
	APIKeys                        []string                             `json:"api_keys"`
	DeleteAPIKeyHashes             []string                             `json:"delete_api_key_hashes"`
	ClearAPIKeys                   bool                                 `json:"clear_api_keys"`
	TimeoutMS                      int                                  `json:"timeout_ms"`
	SampleRate                     int                                  `json:"sample_rate"`
	AllGroups                      bool                                 `json:"all_groups"`
	Groups                         []string                             `json:"groups"`
	RecordNonHits                  bool                                 `json:"record_non_hits"`
	Thresholds                     map[string]float64                   `json:"thresholds"`
	WorkerCount                    int                                  `json:"worker_count"`
	QueueSize                      int                                  `json:"queue_size"`
	BlockStatus                    int                                  `json:"block_status"`
	BlockMessage                   string                               `json:"block_message"`
	EmailOnHit                     bool                                 `json:"email_on_hit"`
	AutoBanEnabled                 bool                                 `json:"auto_ban_enabled"`
	BanThreshold                   int                                  `json:"ban_threshold"`
	ViolationWindowHours           int                                  `json:"violation_window_hours"`
	RetryCount                     int                                  `json:"retry_count"`
	HitRetentionDays               int                                  `json:"hit_retention_days"`
	NonHitRetentionDays            int                                  `json:"non_hit_retention_days"`
	PreHashCheckEnabled            bool                                 `json:"pre_hash_check_enabled"`
	BlockedKeywords                []string                             `json:"blocked_keywords"`
	KeywordBlockingMode            string                               `json:"keyword_blocking_mode"`
	ModelFilter                    setting.ContentModerationModelFilter `json:"model_filter"`
	CyberPolicyExcludeFromBanCount bool                                 `json:"cyber_policy_exclude_from_ban_count"`
	CyberSessionBlockEnabled       bool                                 `json:"cyber_session_block_enabled"`
	CyberSessionBlockTTLSeconds    int                                  `json:"cyber_session_block_ttl_seconds"`
}

func GetContentModerationConfig(c *gin.Context) {
	config, err := setting.GetContentModerationStorageConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "content moderation config is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": contentModerationPublicConfigFromStorage(config)})
}

func UpdateContentModerationConfig(c *gin.Context) {
	var request contentModerationUpdateConfig
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid content moderation config"})
		return
	}
	current, err := setting.GetContentModerationStorageConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "content moderation config is unavailable"})
		return
	}
	if current.ConfigVersion != request.ExpectedConfigVersion {
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "content_moderation_config_conflict", "message": "content moderation config was updated by another administrator"})
		return
	}

	ciphertexts := append([]string(nil), current.APIKeyCiphertexts...)
	statuses := service.ContentModerationAPIKeyStatuses(current)
	deleteHashes := make(map[string]struct{}, len(request.DeleteAPIKeyHashes))
	for _, keyHash := range request.DeleteAPIKeyHashes {
		deleteHashes[strings.TrimSpace(keyHash)] = struct{}{}
	}
	if request.ClearAPIKeys {
		ciphertexts = nil
	} else if len(deleteHashes) > 0 {
		kept := make([]string, 0, len(ciphertexts))
		for index, ciphertext := range ciphertexts {
			if index < len(statuses) {
				if _, remove := deleteHashes[statuses[index].KeyHash]; remove {
					continue
				}
			}
			kept = append(kept, ciphertext)
		}
		ciphertexts = kept
	}
	existingKeyHashes := make(map[string]struct{}, len(ciphertexts))
	for _, ciphertext := range ciphertexts {
		key, decryptErr := setting.DecryptPromptAuditToken(ciphertext)
		if decryptErr == nil && strings.TrimSpace(key) != "" {
			existingKeyHashes[service.ContentModerationAPIKeyIdentifier(key)] = struct{}{}
		}
	}
	for _, key := range request.APIKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keyHash := service.ContentModerationAPIKeyIdentifier(key)
		if _, exists := existingKeyHashes[keyHash]; exists {
			continue
		}
		ciphertext, encryptErr := setting.EncryptContentModerationAPIKey(key)
		if encryptErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "content_moderation_encryption_key_required", "message": encryptErr.Error()})
			return
		}
		ciphertexts = append(ciphertexts, ciphertext)
		existingKeyHashes[keyHash] = struct{}{}
	}

	next := setting.ContentModerationStorageConfig{
		Enabled: request.Enabled, Mode: request.Mode, BaseURL: request.BaseURL, Model: request.Model, ProxyURL: request.ProxyURL,
		APIKeyCiphertexts: ciphertexts, TimeoutMS: request.TimeoutMS, SampleRate: request.SampleRate,
		AllGroups: request.AllGroups, Groups: request.Groups, RecordNonHits: request.RecordNonHits,
		Thresholds: request.Thresholds, WorkerCount: request.WorkerCount, QueueSize: request.QueueSize,
		BlockStatus: request.BlockStatus, BlockMessage: request.BlockMessage, EmailOnHit: request.EmailOnHit,
		AutoBanEnabled: request.AutoBanEnabled, BanThreshold: request.BanThreshold,
		ViolationWindowHours: request.ViolationWindowHours, RetryCount: request.RetryCount,
		HitRetentionDays: request.HitRetentionDays, NonHitRetentionDays: request.NonHitRetentionDays,
		PreHashCheckEnabled: request.PreHashCheckEnabled, BlockedKeywords: request.BlockedKeywords,
		KeywordBlockingMode: request.KeywordBlockingMode, ModelFilter: request.ModelFilter,
		CyberPolicyExcludeFromBanCount: request.CyberPolicyExcludeFromBanCount,
		CyberSessionBlockEnabled:       request.CyberSessionBlockEnabled,
		CyberSessionBlockTTLSeconds:    request.CyberSessionBlockTTLSeconds,
		ConfigVersion:                  current.ConfigVersion + 1, UpdatedAt: time.Now().UTC(),
		UpdatedBy: common.GetContextKeyInt(c, constant.ContextKeyUserId),
	}
	setting.NormalizeContentModerationStorageConfig(&next)
	if err := setting.ValidateContentModerationStorageConfig(next); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	raw, err := json.Marshal(next)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to encode content moderation config"})
		return
	}
	if err := model.UpdateSecurityAuditOptionCAS(setting.ContentModerationConfigOptionKey, request.ExpectedConfigVersion, string(raw)); err != nil {
		status := http.StatusInternalServerError
		code := "content_moderation_config_save_failed"
		message := "failed to save content moderation config"
		switch {
		case errors.Is(err, model.ErrOptionVersionConflict):
			status, code, message = http.StatusConflict, "content_moderation_config_conflict", "content moderation config was updated by another administrator"
		case errors.Is(err, model.ErrSecurityAuditEngineConflict):
			status, code, message = http.StatusConflict, "security_audit_engine_conflict", "content moderation and prompt audit cannot be enabled at the same time"
		}
		c.JSON(status, gin.H{"success": false, "code": code, "message": message})
		return
	}
	service.PublishContentModerationConfigInvalidation(c.Request.Context(), next.ConfigVersion)
	recordManageAudit(c, "content_moderation.config_update", map[string]interface{}{
		"from_version": current.ConfigVersion, "to_version": next.ConfigVersion, "enabled": next.Enabled,
		"mode": next.Mode, "api_key_count": len(next.APIKeyCiphertexts), "group_count": len(next.Groups),
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": contentModerationPublicConfigFromStorage(next)})
}

func contentModerationPublicConfigFromStorage(config setting.ContentModerationStorageConfig) contentModerationPublicConfig {
	statuses := service.ContentModerationAPIKeyStatuses(config)
	modelFilter := config.ModelFilter
	modelFilter.Models = append([]string{}, config.ModelFilter.Models...)
	return contentModerationPublicConfig{
		Enabled: config.Enabled, Mode: config.Mode, BaseURL: config.BaseURL, Model: config.Model, ProxyURL: config.ProxyURL,
		APIKeyCount: len(statuses), APIKeyStatuses: statuses, TimeoutMS: config.TimeoutMS,
		SampleRate: config.SampleRate, AllGroups: config.AllGroups, Groups: append([]string{}, config.Groups...),
		RecordNonHits: config.RecordNonHits, Thresholds: config.Thresholds, WorkerCount: config.WorkerCount,
		QueueSize: config.QueueSize, BlockStatus: config.BlockStatus, BlockMessage: config.BlockMessage,
		EmailOnHit: config.EmailOnHit, AutoBanEnabled: config.AutoBanEnabled, BanThreshold: config.BanThreshold,
		ViolationWindowHours: config.ViolationWindowHours, RetryCount: config.RetryCount,
		HitRetentionDays: config.HitRetentionDays, NonHitRetentionDays: config.NonHitRetentionDays,
		PreHashCheckEnabled: config.PreHashCheckEnabled, BlockedKeywords: append([]string{}, config.BlockedKeywords...),
		KeywordBlockingMode: config.KeywordBlockingMode, ModelFilter: modelFilter,
		CyberPolicyExcludeFromBanCount: config.CyberPolicyExcludeFromBanCount,
		CyberSessionBlockEnabled:       config.CyberSessionBlockEnabled,
		CyberSessionBlockTTLSeconds:    config.CyberSessionBlockTTLSeconds,
		ConfigVersion:                  config.ConfigVersion, UpdatedAt: config.UpdatedAt, UpdatedBy: config.UpdatedBy,
		EncryptionKeyConfigured: setting.PromptAuditEncryptionKeyConfigured(),
		PromptAuditActive:       setting.PromptAuditJSONActive(setting.PromptAuditConfigJSON),
	}
}

func TestContentModerationKeys(c *gin.Context) {
	var request struct {
		APIKeys   []string `json:"api_keys"`
		BaseURL   string   `json:"base_url"`
		Model     string   `json:"model"`
		ProxyURL  *string  `json:"proxy_url"`
		TimeoutMS int      `json:"timeout_ms"`
		Prompt    string   `json:"prompt"`
		Images    []string `json:"images"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid content moderation key test"})
		return
	}
	config, err := setting.GetContentModerationStorageConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "content moderation config is unavailable"})
		return
	}
	if strings.TrimSpace(request.BaseURL) != "" {
		config.BaseURL = strings.TrimSpace(request.BaseURL)
	}
	if strings.TrimSpace(request.Model) != "" {
		config.Model = strings.TrimSpace(request.Model)
	}
	if request.TimeoutMS > 0 {
		config.TimeoutMS = request.TimeoutMS
	}
	if request.ProxyURL != nil {
		config.ProxyURL = strings.TrimSpace(*request.ProxyURL)
	}
	setting.NormalizeContentModerationStorageConfig(&config)
	if err := setting.ValidateContentModerationStorageConfig(config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	result, err := service.TestContentModerationAPIKeys(c.Request.Context(), config, request.APIKeys, request.Prompt, request.Images)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "content_moderation.api_keys_test", map[string]interface{}{
		"provided_key_count": len(request.APIKeys), "result_count": len(result.Items), "image_count": result.ImageCount,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func GetContentModerationRuntime(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": service.GetContentModerationRuntimeStatus()})
}

func ListContentModerationLogs(c *gin.Context) {
	page := contentModerationQueryInt(c, "page", 1)
	pageSize := contentModerationQueryInt(c, "page_size", 20)
	filter := model.ContentModerationLogFilter{
		Result: strings.TrimSpace(c.Query("result")), Group: strings.TrimSpace(c.Query("group")),
		Endpoint: strings.TrimSpace(c.Query("endpoint")), Search: strings.TrimSpace(c.Query("search")),
		StartTime: int64(contentModerationQueryInt(c, "start_time", 0)), EndTime: int64(contentModerationQueryInt(c, "end_time", 0)),
	}
	logs, total, err := model.ListContentModerationLogs(filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to list content moderation logs"})
		return
	}
	items := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		items = append(items, service.DecodeContentModerationLog(log))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "page": page, "page_size": pageSize}})
}

func UnbanContentModerationUser(c *gin.Context) {
	userID, err := strconv.Atoi(strings.TrimSpace(c.Param("user_id")))
	if err != nil || userID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user id"})
		return
	}
	if err := model.SetUserStatusForContentModeration(userID, common.UserStatusEnabled); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "content_moderation.user_unban", map[string]interface{}{"user_id": userID})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"user_id": userID, "status": common.UserStatusEnabled}})
}

func DeleteContentModerationHash(c *gin.Context) {
	var request struct {
		InputHash string `json:"input_hash"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid content moderation hash"})
		return
	}
	inputHash := strings.TrimSpace(request.InputHash)
	_, decodeErr := hex.DecodeString(inputHash)
	if decodeErr != nil || len(inputHash) != 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid content moderation hash"})
		return
	}
	deleted, err := model.DeleteContentModerationHash(c.Request.Context(), inputHash)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "content_moderation.hash_delete", map[string]interface{}{"input_hash": inputHash, "deleted": deleted})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"input_hash": inputHash, "deleted": deleted}})
}

func ClearContentModerationHashes(c *gin.Context) {
	deleted, err := model.ClearContentModerationHashes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "content_moderation.hashes_clear", map[string]interface{}{"deleted": deleted})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"deleted": deleted}})
}

func contentModerationQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}

func contentModerationJSONBody(c *gin.Context, body []byte) ([]byte, error) {
	if json.Valid(body) {
		return body, nil
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if !strings.Contains(contentType, gin.MIMEMultipartPOSTForm) {
		return promptAuditJSONBody(c, body)
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	values := make(map[string]any, len(form.Value)+1)
	for key, items := range form.Value {
		if len(items) == 1 {
			values[key] = items[0]
		} else if len(items) > 1 {
			values[key] = items
		}
	}
	const maxImageBytes = 8 << 20
	images := make([]string, 0, 1)
	for _, headers := range form.File {
		for _, header := range headers {
			file, openErr := header.Open()
			if openErr != nil {
				return nil, openErr
			}
			data, readErr := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
			_ = file.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(data) > maxImageBytes {
				return nil, errors.New("content moderation image exceeds 8 MiB")
			}
			mediaType := strings.TrimSpace(header.Header.Get("Content-Type"))
			if mediaType == "" || mediaType == "application/octet-stream" {
				mediaType = http.DetectContentType(data)
			}
			if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
				continue
			}
			images = append(images, "data:"+mediaType+";base64,"+base64.StdEncoding.EncodeToString(data))
			break
		}
		if len(images) > 0 {
			break
		}
	}
	if len(images) > 0 {
		values["images"] = images
	}
	if len(values) == 0 {
		return nil, nil
	}
	return json.Marshal(values)
}
