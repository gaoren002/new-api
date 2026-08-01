package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	contentModerationCyberMarkKey     = "content_moderation_cyber_policy"
	contentModerationCyberRedisPrefix = "newapi:content_moderation:cyber_session:"
)

type CyberPolicyMark struct {
	Message              string
	Body                 string
	UpstreamStatus       int
	UpstreamInputTokens  int
	UpstreamOutputTokens int
	QuotaClamp           *common.QuotaClamp
}

func DetectCyberPolicyPayload(payload []byte) (bool, string) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return false, ""
	}
	code := gjson.GetBytes(payload, "error.code").String()
	if code == "" {
		code = gjson.GetBytes(payload, "response.error.code").String()
	}
	if !strings.EqualFold(strings.TrimSpace(code), "cyber_policy") {
		return false, ""
	}
	message := gjson.GetBytes(payload, "error.message").String()
	if message == "" {
		message = gjson.GetBytes(payload, "response.error.message").String()
	}
	return true, strings.TrimSpace(message)
}

func ExtractCyberPolicyUsage(payload []byte) (int, int) {
	input, output, _ := extractCyberPolicyUsageChecked(payload)
	return input, output
}

func extractCyberPolicyUsageChecked(payload []byte) (int, int, *common.QuotaClamp) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return 0, 0, nil
	}
	inputTokens, clamp := firstCyberPolicyToken(payload,
		"response.usage.input_tokens", "usage.input_tokens",
		"response.usage.prompt_tokens", "usage.prompt_tokens",
	)
	outputTokens, outputClamp := firstCyberPolicyToken(payload,
		"response.usage.output_tokens", "usage.output_tokens",
		"response.usage.completion_tokens", "usage.completion_tokens",
	)
	if clamp == nil {
		clamp = outputClamp
	}
	return inputTokens, outputTokens, clamp
}

func firstCyberPolicyToken(payload []byte, paths ...string) (int, *common.QuotaClamp) {
	for _, path := range paths {
		value := gjson.GetBytes(payload, path)
		if value.Exists() && value.Float() > 0 {
			return common.QuotaFromFloatChecked(value.Float())
		}
	}
	return 0, nil
}

func MarkCyberPolicy(c *gin.Context, mark CyberPolicyMark) {
	if c == nil {
		return
	}
	if existing := GetCyberPolicyMark(c); existing != nil {
		return
	}
	mark.Message = strings.TrimSpace(mark.Message)
	mark.Body = strings.TrimSpace(mark.Body)
	inputTokens, outputTokens, clamp := extractCyberPolicyUsageChecked([]byte(mark.Body))
	mark.QuotaClamp = clamp
	if mark.UpstreamInputTokens <= 0 && mark.UpstreamOutputTokens <= 0 {
		mark.UpstreamInputTokens, mark.UpstreamOutputTokens = inputTokens, outputTokens
	}
	mark.Body = contentModerationTrimRunes(mark.Body, 4000)
	c.Set(contentModerationCyberMarkKey, &mark)
}

func GetCyberPolicyMark(c *gin.Context) *CyberPolicyMark {
	if c == nil {
		return nil
	}
	value, exists := c.Get(contentModerationCyberMarkKey)
	if !exists {
		return nil
	}
	mark, _ := value.(*CyberPolicyMark)
	return mark
}

func CyberSessionBlockKey(tokenID int, c *gin.Context, body []byte) string {
	if c == nil {
		return ""
	}
	var raw string
	for _, header := range []string{
		"session_id", "conversation_id", "X-Session-Affinity", "X-Session-Id",
		"X-OpenCode-Session", "X-Conversation-ID",
	} {
		if raw = strings.TrimSpace(c.GetHeader(header)); raw != "" {
			break
		}
	}
	if raw == "" && len(body) > 0 {
		raw = strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	}
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("t%d:%s", tokenID, raw)))
	return hex.EncodeToString(sum[:])
}

func IsCyberSessionBlocked(ctx context.Context, config setting.ContentModerationStorageConfig, key string) bool {
	if !config.CyberSessionBlockEnabled || key == "" || !common.RedisEnabled || common.RDB == nil {
		return false
	}
	count, err := common.RDB.Exists(ctx, contentModerationCyberRedisPrefix+key).Result()
	return err == nil && count > 0
}

func MarkCyberSessionBlocked(ctx context.Context, config setting.ContentModerationStorageConfig, key string) {
	if !config.CyberSessionBlockEnabled || key == "" || !common.RedisEnabled || common.RDB == nil {
		return
	}
	ttl := time.Duration(config.CyberSessionBlockTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Duration(setting.DefaultCyberSessionBlockTTL) * time.Second
	}
	writeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := common.RDB.Set(writeCtx, contentModerationCyberRedisPrefix+key, "1", ttl).Err(); err != nil {
		common.SysError("content moderation cyber session block write failed: " + err.Error())
	}
}

func RecordCyberPolicyEvent(ctx context.Context, input ContentModerationCheckInput, mark CyberPolicyMark) {
	config, err := setting.GetContentModerationStorageConfig()
	if err != nil || !setting.ContentModerationCyberPolicyActive(config) {
		return
	}
	errorText := strings.TrimSpace(mark.Message)
	if mark.Body != "" {
		errorText = strings.TrimSpace(errorText + "\n" + mark.Body)
	}
	if mark.UpstreamInputTokens > 0 || mark.UpstreamOutputTokens > 0 {
		errorText = fmt.Sprintf("%s\nupstream_usage=in:%d,out:%d", errorText, mark.UpstreamInputTokens, mark.UpstreamOutputTokens)
	}
	log := buildContentModerationLog(
		input, config, ContentModerationActionCyberPolicy, true, "cyber_policy", 1, "",
		map[string]float64{"cyber_policy": 1}, "", nil, nil, errorText,
	)
	log.Mode = "post_upstream"
	log.Error = redactPromptAuditValue(errorText, 1000)
	if !config.CyberPolicyExcludeFromBanCount && log.UserId > 0 {
		since := time.Now().Add(-time.Duration(config.ViolationWindowHours) * time.Hour).Unix()
		count, countErr := model.CountContentModerationViolations(ctx, log.UserId, since, false)
		if countErr == nil {
			log.ViolationCount = int(count) + 1
		}
		if config.AutoBanEnabled && log.ViolationCount >= config.BanThreshold {
			if statusErr := model.SetUserStatusForContentModeration(log.UserId, common.UserStatusDisabled); statusErr == nil {
				log.AutoBanned = true
			}
		}
	}
	logPersisted := true
	if err := model.CreateContentModerationLog(ctx, log); err != nil {
		logPersisted = false
		contentModerationRuntime.errors.Add(1)
		common.SysError("content moderation cyber policy log write failed: " + err.Error())
	}
	emailSent := false
	if strings.TrimSpace(log.UserEmail) != "" {
		subject := fmt.Sprintf("[%s] 网络安全策略拦截 / Cyber Policy Notice", common.SystemName)
		body := fmt.Sprintf(
			"<p>您的请求被上游网络安全策略拦截。</p><p>模型：%s<br>分组：%s<br>上游消息：%s<br>请求时间：%s</p>",
			html.EscapeString(log.Model), html.EscapeString(log.GroupName), html.EscapeString(mark.Message), time.Now().Format(time.RFC3339),
		)
		if emailErr := common.SendEmail(subject, log.UserEmail, body); emailErr == nil {
			emailSent = true
		}
	}
	if logPersisted && emailSent {
		if err := model.SetContentModerationLogEmailSent(ctx, log.Id); err != nil {
			contentModerationRuntime.errors.Add(1)
			common.SysError("content moderation cyber policy email status update failed: " + err.Error())
		}
	}
}
