package service

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// DecideRelayRetry is the single retry decision for relay attempts. The reason
// is recorded in the request policy decision events of the log details.
func DecideRelayRetry(c *gin.Context, err *types.NewAPIError, retryTimes int) PolicyDecision {
	if err == nil {
		return PolicyDecision{Action: "stop", Reason: "request_completed", Source: "system"}
	}
	if ShouldSkipRetryAfterChannelAffinityFailure(c) {
		source := RequestPolicy(c).SessionModeSource
		if source == "" {
			source = "session_rule"
		}
		return PolicyDecision{Action: "stop", Reason: "strict_session", Source: source}
	}
	if GetChannelConstraints(c).SuppressesRetry() {
		return PolicyDecision{Action: "stop", Reason: "pinned_channel", Source: "channel_constraint"}
	}
	if types.IsChannelError(err) {
		return PolicyDecision{Action: "retry", Reason: "channel_error", Source: "system"}
	}
	if types.IsSkipRetryError(err) {
		return PolicyDecision{Action: "stop", Reason: "non_retryable_error", Source: "system"}
	}
	if retryTimes <= 0 {
		return PolicyDecision{Action: "stop", Reason: "attempt_budget_exhausted", Source: "global"}
	}
	code := err.StatusCode
	if code >= 200 && code < 300 {
		return PolicyDecision{Action: "stop", Reason: "system_retry_exclusion", Source: "system"}
	}
	if code < 100 || code > 599 {
		return PolicyDecision{Action: "retry", Reason: "unrecognized_status", Source: "system"}
	}
	// The relay gave up on an upstream that sent nothing in time and nothing
	// reached the client, so unlike an upstream 504 the request moves on as the
	// timeout settings ask, even though the abandoned upstream may still finish
	// and bill its attempt.
	if err.GetErrorCode() == types.ErrorCodeFirstTokenTimeout {
		return PolicyDecision{Action: "retry", Reason: "first_token_timeout", Source: "global"}
	}
	if operation_setting.IsAlwaysSkipRetryCode(err.GetErrorCode()) || operation_setting.IsAlwaysSkipRetryStatusCode(code) {
		return PolicyDecision{Action: "stop", Reason: "system_retry_exclusion", Source: "system"}
	}
	if operation_setting.ShouldRetryByStatusCode(code) {
		return PolicyDecision{Action: "retry", Reason: "retry_status_matched", Source: "global"}
	}
	return PolicyDecision{Action: "stop", Reason: "status_not_retryable", Source: "global"}
}

// FallbackAvailable reports whether a failure of the current attempt would move
// the request to another channel: the retry policy allows another attempt and
// the fallback chain still has an untried channel. The upstream timeouts only
// fire when it does, so they never turn the last channel's slow answer into a
// failure.
func FallbackAvailable(c *gin.Context, retryParam *RetryParam) bool {
	if ShouldSkipRetryAfterChannelAffinityFailure(c) || GetChannelConstraints(c).SuppressesRetry() {
		return false
	}
	return retryParam.HasFallbackChannel()
}

// channelCircuitReasonRunes keeps the last failure shown in the channel list
// tooltip readable.
const channelCircuitReasonRunes = 200

// RecordChannelHealth feeds one relay attempt into the circuit of the channel
// for the requested model: a success ends its failure run, a channel failure
// extends it and may open it.
func RecordChannelHealth(c *gin.Context, channelID int, modelName string, err *types.NewAPIError) {
	if err == nil {
		if model.RecordChannelSuccess(channelID, modelName) {
			logger.LogInfo(c, common.LogText("channel #%d circuit closed for %s: a request succeeded", channelID, modelName))
		}
		return
	}
	if !isChannelFailure(c, err) {
		return
	}
	reason := err.MaskSensitiveErrorWithStatusCode(common.UpstreamAddresses(c)...)
	if runes := []rune(reason); len(runes) > channelCircuitReasonRunes {
		reason = string(runes[:channelCircuitReasonRunes]) + "…"
	}
	if model.RecordChannelFailure(channelID, modelName, reason) {
		logger.LogWarn(c, common.LogText("channel #%d circuit open for %s after %d consecutive failures, last: %s", channelID, modelName, common.ChannelCircuitFailureThreshold, reason))
	}
}

// upstreamModelRefusals are answers, usually a 400, with which an upstream says
// it cannot serve the requested model at all: its router does not know the
// model, the account type behind the channel cannot use it, or an upstream
// new-api has not priced it. Matched in lower case.
var upstreamModelRefusals = []string{
	"unknown provider for model",
	"is not supported when using codex with a chatgpt account",
	"has not been priced by the administrator",
	"的价格尚未由管理员配置",
	"的價格尚未由管理員設定",
}

// isChannelFailure reports whether a failed attempt counts against the
// channel for the model: timeouts, transport and channel errors, upstream 401,
// 402, 403, 404, 405, 408, 429 or 5xx answers, and answers saying the upstream
// cannot serve the model. Circuits are kept per channel and model, so a model
// the upstream refuses says nothing about the channel's other models. A
// request the client abandoned does not count, nor one the gateway or the
// upstream rejected for the request itself, such as a 400 for bad parameters,
// a 413 for a body too large or a 426 for an outdated client.
func isChannelFailure(c *gin.Context, err *types.NewAPIError) bool {
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	switch err.GetErrorCode() {
	case types.ErrorCodeDoRequestFailed, types.ErrorCodeFirstTokenTimeout:
		return true
	}
	if types.IsSkipRetryError(err) || err.GetErrorType() == types.ErrorTypeNewAPIError {
		return false
	}
	switch code := err.StatusCode; code {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound,
		http.StatusMethodNotAllowed, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		if code >= http.StatusInternalServerError {
			return true
		}
	}
	message := strings.ToLower(err.Error())
	return slices.ContainsFunc(upstreamModelRefusals, func(refusal string) bool {
		return strings.Contains(message, refusal)
	})
}

func ShouldRetryRelayError(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	return DecideRelayRetry(c, openaiErr, retryTimes).Action == "retry"
}

func ProcessChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, relayInfo *relaycommon.RelayInfo) {
	if err == nil {
		return
	}
	upstreamURLs := common.UpstreamAddresses(c)
	logger.LogError(c, common.LogText("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.MaskSensitiveErrorWithStatusCode(upstreamURLs...))))
	if ShouldDisableChannel(err) && channelError.AutoBan {
		reason := err.MaskSensitiveErrorWithStatusCode(upstreamURLs...)
		gopool.Go(func() {
			DisableChannel(channelError, reason)
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		other := model.NewLogOther()
		if c.Request != nil && c.Request.URL != nil {
			other.SetPublic("request_path", c.Request.URL.Path)
		}
		other.SetPublic("error_type", err.GetErrorType())
		other.SetPublic("error_code", err.GetErrorCode())
		other.SetPublic("status_code", err.StatusCode)
		AppendRelayLogAdminInfo(c, relayInfo, other)
		AppendResponseModelLogInfo(relayInfo, other)
		AppendTaskPluginContextAuditInfo(c, other)
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelError.ChannelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(upstreamURLs...), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}
}
