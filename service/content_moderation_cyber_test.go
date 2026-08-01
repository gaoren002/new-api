package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestDetectCyberPolicyPayload(t *testing.T) {
	tests := []struct {
		payload string
		hit     bool
		message string
	}{
		{`{"error":{"code":"cyber_policy","message":"blocked"}}`, true, "blocked"},
		{`{"response":{"error":{"code":"CYBER_POLICY","message":" denied "}}}`, true, "denied"},
		{`{"error":{"code":"content_policy","message":"other"}}`, false, ""},
		{`not-json`, false, ""},
	}
	for _, test := range tests {
		hit, message := DetectCyberPolicyPayload([]byte(test.payload))
		require.Equal(t, test.hit, hit)
		require.Equal(t, test.message, message)
	}
}

func TestExtractCyberPolicyUsage(t *testing.T) {
	tests := []struct {
		payload string
		input   int
		output  int
	}{
		{`{"usage":{"input_tokens":18446744073709551615,"output_tokens":18446744073709551615}}`, common.MaxQuota, common.MaxQuota},
		{`{"usage":{"input_tokens":-1,"output_tokens":-2}}`, 0, 0},
		{`{"response":{"usage":{"input_tokens":1234,"output_tokens":7}}}`, 1234, 7},
		{`{"usage":{"prompt_tokens":11,"completion_tokens":3}}`, 11, 3},
		{`{"response":{"error":{"code":"cyber_policy"}}}`, 0, 0},
	}
	for _, test := range tests {
		inputTokens, outputTokens := ExtractCyberPolicyUsage([]byte(test.payload))
		require.Equal(t, test.input, inputTokens)
		require.Equal(t, test.output, outputTokens)
	}
}

func TestCyberPolicySaturationIsCarriedToRequestAudit(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	MarkCyberPolicy(c, CyberPolicyMark{Body: `{"usage":{"input_tokens":18446744073709551615,"output_tokens":1}}`})
	mark := GetCyberPolicyMark(c)
	require.NotNil(t, mark)
	require.Equal(t, common.MaxQuota, mark.UpstreamInputTokens)
	require.NotNil(t, mark.QuotaClamp)
	require.Equal(t, common.QuotaClampOverflow, mark.QuotaClamp.Kind)
}

func TestCyberPolicyWithoutUsageRefundsTieredBaseAndToolFees(t *testing.T) {
	userSetting := configureDataConsentBillingTest(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })
	operation_setting.SetToolPriceForTest(dto.BuildInToolWebSearch, 100)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest(dto.BuildInToolWebSearch) })
	for _, usage := range []*dto.Usage{nil, {UsageSource: "upstream"}} {
		name := "empty upstream usage"
		if usage == nil {
			name = "missing upstream usage"
		}
		t.Run(name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 880, 1_000)
			seedChannel(t, 880)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set("claude_web_search_requests", 1)
			MarkCyberPolicy(c, CyberPolicyMark{Message: "blocked"})
			const expression = `tier("base", 1000000)`
			info := &relaycommon.RelayInfo{
				UserId: 880, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 880}, IsPlayground: true,
				UserSetting: userSetting, OriginModelName: "cyber-billing-test", StartTime: time.Now(),
				FinalPreConsumedQuota: 200,
				PriceData:             hosttypes.PriceData{ModelRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{
					BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), GroupRatio: 1, QuotaPerUnit: common.QuotaPerUnit,
				},
			}
			info.SetEstimatePromptTokens(500)
			info.Billing = &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 880, consumed: 200}, preConsumedQuota: 200}
			PostTextConsumeQuota(c, info, usage, nil)
			require.Equal(t, 1_200, getUserQuota(t, 880), "the complete reservation is refunded despite configured base/tool fees")
			log := getLastLog(t)
			require.NotNil(t, log)
			require.Zero(t, log.Quota)
		})
	}
}

func TestRelayErrorHandlerMarksCyberPolicyAsNonRetryable(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"cyber_policy","type":"invalid_request_error","message":"blocked"}}`)),
	}
	err := RelayErrorHandler(t.Context(), response, false)
	require.Equal(t, types.ErrorCodeCyberPolicy, err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
	require.Equal(t, "blocked", err.ToOpenAIError().Message)
}

func TestCyberSessionBlockKeyUsesOnlyExplicitSessionSignals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func(header, body string) (*gin.Context, []byte) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		if header != "" {
			c.Request.Header.Set("session_id", header)
		}
		return c, []byte(body)
	}
	c1, body1 := newContext("session-a", `{}`)
	c2, body2 := newContext("session-a", `{}`)
	require.Equal(t, CyberSessionBlockKey(7, c1, body1), CyberSessionBlockKey(7, c2, body2))
	require.NotEqual(t, CyberSessionBlockKey(7, c1, body1), CyberSessionBlockKey(8, c2, body2))
	c3, body3 := newContext("", `{"prompt_cache_key":"cache-a"}`)
	require.NotEmpty(t, CyberSessionBlockKey(7, c3, body3))
	c4, body4 := newContext("", `{"input":"hello"}`)
	require.Empty(t, CyberSessionBlockKey(7, c4, body4))
}

func TestMarkCyberPolicyKeepsFirstEvent(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	MarkCyberPolicy(c, CyberPolicyMark{Message: "first", Body: `{"usage":{"input_tokens":9,"output_tokens":2}}`})
	MarkCyberPolicy(c, CyberPolicyMark{Message: "second"})
	require.Equal(t, "first", GetCyberPolicyMark(c).Message)
	require.Equal(t, 9, GetCyberPolicyMark(c).UpstreamInputTokens)
	require.Equal(t, 2, GetCyberPolicyMark(c).UpstreamOutputTokens)
}

func TestCyberSessionBlockRoundTripAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	previousEnabled, previousClient := common.RedisEnabled, common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled, common.RDB = previousEnabled, previousClient
	})

	config := setting.DefaultContentModerationStorageConfig()
	config.CyberSessionBlockEnabled = true
	config.CyberSessionBlockTTLSeconds = 90
	MarkCyberSessionBlocked(context.Background(), config, "session-key")
	require.True(t, IsCyberSessionBlocked(context.Background(), config, "session-key"))
	require.Equal(t, 90*time.Second, server.TTL(contentModerationCyberRedisPrefix+"session-key"))

	server.FastForward(91 * time.Second)
	require.False(t, IsCyberSessionBlocked(context.Background(), config, "session-key"))
}
