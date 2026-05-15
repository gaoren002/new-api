package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	internalReviewScopeText  = "text"
	internalReviewScopeImage = "image"
)

type InternalReviewDecision struct {
	Allowed bool
	Blocked bool
	Reason  string
	Code    string
	Raw     map[string]any
}

type internalReviewRequest struct {
	RequestID    string         `json:"request_id,omitempty"`
	UserID       int            `json:"user_id,omitempty"`
	TokenID      int            `json:"token_id,omitempty"`
	Model        string         `json:"model,omitempty"`
	Group        string         `json:"group,omitempty"`
	Path         string         `json:"path,omitempty"`
	RelayFormat  string         `json:"relay_format,omitempty"`
	RelayMode    int            `json:"relay_mode,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	Stream       bool           `json:"stream,omitempty"`
	Text         string         `json:"text,omitempty"`
	Request      dto.Request    `json:"request,omitempty"`
	TokenMeta    map[string]any `json:"token_meta,omitempty"`
	DataConsent  map[string]any `json:"data_consent,omitempty"`
	RequestEpoch int64          `json:"request_epoch,omitempty"`
}

type internalReviewResponse struct {
	Allowed  *bool          `json:"allowed,omitempty"`
	Blocked  *bool          `json:"blocked,omitempty"`
	Flagged  *bool          `json:"flagged,omitempty"`
	Decision string         `json:"decision,omitempty"`
	Action   string         `json:"action,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Message  string         `json:"message,omitempty"`
	Code     string         `json:"code,omitempty"`
	Error    any            `json:"error,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

func ShouldKeepBillingForError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return err.GetErrorCode() == types.ErrorCodeInternalReviewBlocked
}

func IsInternalReviewApplied(relayInfo *relaycommon.RelayInfo) bool {
	if relayInfo == nil {
		return false
	}
	return relayInfo.InternalReviewApplied
}

func InternalReviewDecisionFromRelayInfo(relayInfo *relaycommon.RelayInfo) *InternalReviewDecision {
	if relayInfo == nil || !relayInfo.InternalReviewApplied {
		return nil
	}
	return &InternalReviewDecision{
		Allowed: relayInfo.InternalReviewAllowed,
		Blocked: relayInfo.InternalReviewBlocked,
		Reason:  relayInfo.InternalReviewReason,
		Code:    relayInfo.InternalReviewCode,
	}
}

func ShouldPrepareInternalReviewMeta(relayInfo *relaycommon.RelayInfo, request dto.Request) bool {
	if relayInfo == nil || request == nil {
		return false
	}
	if !operation_setting.IsInternalReviewEnabled() {
		return false
	}
	scope := internalReviewScopeForRequest(request)
	if scope == "" || !operation_setting.IsInternalReviewScopeEnabled(scope) {
		return false
	}
	return internalReviewModelAllowed(relayInfo.OriginModelName, operation_setting.GetInternalReviewModelFilter())
}

func RecordInternalReviewBlockedConsume(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, decision *InternalReviewDecision, meta *types.TokenCountMeta) {
	if ctx == nil || relayInfo == nil {
		return
	}
	quota := relayInfo.FinalPreConsumedQuota
	if quota <= 0 {
		quota = relayInfo.PriceData.QuotaToPreConsume
	}
	if quota < 0 {
		quota = 0
	}
	if err := SettleBilling(ctx, relayInfo, quota); err != nil {
		logger.LogError(ctx, "error settling internal review blocked billing: "+err.Error())
	}
	if quota > 0 {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	reason := ""
	code := ""
	if decision != nil {
		reason = strings.TrimSpace(decision.Reason)
		code = strings.TrimSpace(decision.Code)
	}
	if reason == "" {
		reason = "blocked by internal review"
	}
	relayInfo.SetFirstResponseTime()
	other := GenerateTextOtherInfo(ctx, relayInfo, relayInfo.PriceData.ModelRatio, relayInfo.PriceData.GroupRatioInfo.GroupRatio, relayInfo.PriceData.CompletionRatio, 0, relayInfo.PriceData.CacheRatio, relayInfo.PriceData.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	other["internal_review"] = true
	other["internal_review_blocked"] = true
	other["internal_review_reason"] = reason
	if code != "" {
		other["internal_review_code"] = code
	}
	other["internal_review_billing"] = "pre_consumed"

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:      relayInfo.ChannelId,
		PromptTokens:   relayInfo.GetEstimatePromptTokens(),
		ModelName:      relayInfo.OriginModelName,
		TokenName:      ctx.GetString("token_name"),
		Quota:          quota,
		Content:        fmt.Sprintf("Internal review blocked: %s", reason),
		TokenId:        relayInfo.TokenId,
		UseTimeSeconds: int(time.Now().Unix() - relayInfo.StartTime.Unix()),
		IsStream:       relayInfo.IsStream,
		Group:          relayInfo.UsingGroup,
		Other:          other,
	})
}

func InternalReviewRequest(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, request dto.Request, meta *types.TokenCountMeta) (*InternalReviewDecision, *types.NewAPIError) {
	if ctx == nil || relayInfo == nil || request == nil {
		return nil, nil
	}
	if !operation_setting.IsInternalReviewEnabled() {
		return nil, nil
	}
	scope := internalReviewScopeForRequest(request)
	if scope == "" || !operation_setting.IsInternalReviewScopeEnabled(scope) {
		return nil, nil
	}
	if !internalReviewModelAllowed(relayInfo.OriginModelName, operation_setting.GetInternalReviewModelFilter()) {
		return nil, nil
	}
	endpoint := operation_setting.GetInternalReviewEndpoint()
	if endpoint == "" {
		return nil, internalReviewConfigError("internal review endpoint is empty")
	}
	decision, err := postInternalReview(ctx, endpoint, buildInternalReviewPayload(ctx, relayInfo, request, meta, scope))
	if err != nil {
		if operation_setting.ShouldInternalReviewFailClosed() {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("internal review failed: %w", err), types.ErrorCodeInternalReviewFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		logger.LogWarn(ctx, "internal review failed open: "+err.Error())
		return nil, nil
	}
	relayInfo.InternalReviewApplied = true
	relayInfo.InternalReviewAllowed = decision.Allowed
	relayInfo.InternalReviewBlocked = decision.Blocked
	relayInfo.InternalReviewReason = decision.Reason
	relayInfo.InternalReviewCode = decision.Code
	if decision.Blocked {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "request blocked by internal review"
		}
		common.SetContextKey(ctx, constant.ContextKeyAdminRejectReason, "internal_review="+reason)
		return decision, types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeInternalReviewBlocked, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	return decision, nil
}

func internalReviewConfigError(message string) *types.NewAPIError {
	if operation_setting.ShouldInternalReviewFailClosed() {
		return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInternalReviewFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func internalReviewScopeForRequest(request dto.Request) string {
	switch request.(type) {
	case *dto.ImageRequest:
		return internalReviewScopeImage
	case *dto.GeneralOpenAIRequest, *dto.OpenAIResponsesRequest, *dto.OpenAIResponsesCompactionRequest, *dto.ClaudeRequest, *dto.GeminiChatRequest:
		return internalReviewScopeText
	default:
		return ""
	}
}

func internalReviewModelAllowed(modelName string, rawFilter string) bool {
	rawFilter = strings.TrimSpace(rawFilter)
	if rawFilter == "" {
		return true
	}
	include := false
	hasInclude := false
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	for _, item := range strings.FieldsFunc(rawFilter, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	}) {
		pattern := strings.ToLower(strings.TrimSpace(item))
		if pattern == "" {
			continue
		}
		exclude := strings.HasPrefix(pattern, "!")
		if exclude {
			pattern = strings.TrimSpace(strings.TrimPrefix(pattern, "!"))
		} else {
			hasInclude = true
		}
		if pattern == "" {
			continue
		}
		matched := pattern == "*" || strings.EqualFold(pattern, modelName) || strings.Contains(modelName, pattern)
		if matched && exclude {
			return false
		}
		if matched {
			include = true
		}
	}
	return include || !hasInclude
}

func buildInternalReviewPayload(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, request dto.Request, meta *types.TokenCountMeta, scope string) internalReviewRequest {
	text := ""
	tokenMeta := map[string]any{}
	if meta != nil {
		text = meta.CombineText
		tokenMeta["max_tokens"] = meta.MaxTokens
		tokenMeta["messages_count"] = meta.MessagesCount
		tokenMeta["tools_count"] = meta.ToolsCount
		tokenMeta["files_count"] = len(meta.Files)
	}
	dataConsent := DataConsentStateForRelayInfo(relayInfo)
	return internalReviewRequest{
		RequestID:   ctx.GetString(common.RequestIdKey),
		UserID:      relayInfo.UserId,
		TokenID:     relayInfo.TokenId,
		Model:       relayInfo.OriginModelName,
		Group:       relayInfo.UsingGroup,
		Path:        ctx.Request.URL.Path,
		RelayFormat: string(relayInfo.RelayFormat),
		RelayMode:   relayInfo.RelayMode,
		Scope:       scope,
		Stream:      request.IsStream(ctx),
		Text:        text,
		Request:     request,
		TokenMeta:   tokenMeta,
		DataConsent: map[string]any{
			"enabled":           dataConsent.Enabled,
			"status":            dataConsent.Status,
			"authorized":        dataConsent.Authorized,
			"price_multiplier":  dataConsent.Multiplier,
			"agreement_version": dataConsent.AgreementVersion,
			"user_version":      dataConsent.UserVersion,
		},
		RequestEpoch: time.Now().Unix(),
	}
}

func postInternalReview(ctx *gin.Context, endpoint string, payload internalReviewRequest) (*InternalReviewDecision, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(endpoint, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, err
	}

	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(operation_setting.GetInternalReviewTimeoutSeconds()) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token := operation_setting.GetInternalReviewBearerToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("internal review status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var reviewResp internalReviewResponse
	if len(bytes.TrimSpace(respBody)) > 0 {
		if err := common.Unmarshal(respBody, &reviewResp); err != nil {
			return nil, err
		}
	}
	return normalizeInternalReviewResponse(reviewResp), nil
}

func normalizeInternalReviewResponse(resp internalReviewResponse) *InternalReviewDecision {
	decision := strings.ToLower(strings.TrimSpace(resp.Decision))
	action := strings.ToLower(strings.TrimSpace(resp.Action))
	reason := strings.TrimSpace(resp.Reason)
	if reason == "" {
		reason = strings.TrimSpace(resp.Message)
	}
	if reason == "" && resp.Error != nil {
		reason = common.Interface2String(resp.Error)
	}

	blocked := false
	if resp.Blocked != nil && *resp.Blocked {
		blocked = true
	}
	if resp.Flagged != nil && *resp.Flagged {
		blocked = true
	}
	if resp.Allowed != nil && !*resp.Allowed {
		blocked = true
	}
	switch decision {
	case "block", "blocked", "deny", "denied", "reject", "rejected":
		blocked = true
	case "allow", "allowed", "pass", "passed":
		blocked = false
	}
	switch action {
	case "block", "deny", "reject":
		blocked = true
	case "allow", "pass":
		blocked = false
	}

	return &InternalReviewDecision{
		Allowed: !blocked,
		Blocked: blocked,
		Reason:  reason,
		Code:    strings.TrimSpace(resp.Code),
		Raw:     resp.Details,
	}
}
