package qwen3guard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Adaptor relays requests to a Qwen3Guard-Gen model served behind any
// OpenAI-compatible chat completions endpoint (Ollama, vLLM, DashScope
// compatible mode, ...).
//
// Two relay modes are supported:
//   - RelayModeModerations: the featured moderation interface. Each input
//     item becomes one upstream guard chat call; verdicts are aggregated
//     into a GuardModerationResponse.
//   - RelayModeChatCompletions: raw pass-through to the guard model, which
//     also keeps the admin channel test working.
type Adaptor struct {
	payloads       []guardChatPayload
	upstreamBodies [][]byte
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := info.ChannelBaseUrl
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeQwen3Guard]
	}
	return service.PromptAuditChatCompletionsURL(baseURL)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Content-Type", "application/json")
	if info.ApiKey != "" {
		req.Set("Authorization", fmt.Sprintf("Bearer %s", info.ApiKey))
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if info.RelayMode != relayconstant.RelayModeModerations {
		// Raw chat access to the guard model (also used by channel testing).
		return request, nil
	}
	targets, err := normalizeModerationInput(request.Input)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// Moderation responses are always plain JSON; a client-supplied
	// "stream": true must not switch the relay into SSE mode.
	info.IsStream = false
	a.payloads = buildGuardChatPayloads(info.UpstreamModelName, targets)
	return a.payloads[0], nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode != relayconstant.RelayModeModerations {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
	if len(a.payloads) == 0 {
		return nil, errors.New("pass-through body is not supported for qwen3guard moderation requests")
	}
	a.upstreamBodies = make([][]byte, 0, len(a.payloads))
	var lastResponse *http.Response
	for _, payload := range a.payloads {
		jsonData, err := common.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal guard payload failed: %w", err)
		}
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return nil, fmt.Errorf("apply param override failed: %w", err)
			}
		}
		resp, err := channel.DoApiRequest(a, c, info, bytes.NewReader(jsonData))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			// Hand the failing upstream response to the relay error path.
			return resp, nil
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxGuardResponseBytes+1))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read guard response failed: %w", err)
		}
		if len(body) > maxGuardResponseBytes {
			return nil, errors.New("guard response exceeds size limit")
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		a.upstreamBodies = append(a.upstreamBodies, body)
		lastResponse = resp
	}
	return lastResponse, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayMode != relayconstant.RelayModeModerations {
		if info.IsStream {
			usage, err = openai.OaiStreamHandler(c, info, resp)
		} else {
			usage, err = openai.OpenaiHandler(c, info, resp)
		}
		return
	}
	results := make([]GuardModerationResult, 0, len(a.upstreamBodies))
	verdicts := make([]string, 0, len(a.upstreamBodies))
	totalUsage := &dto.Usage{}
	for index, body := range a.upstreamBodies {
		verdict, callUsage, extractErr := extractGuardChatContent(body)
		if extractErr != nil {
			return nil, types.NewError(extractErr, types.ErrorCodeBadResponseBody)
		}
		result, buildErr := buildModerationResult(index, verdict)
		if buildErr != nil {
			return nil, types.NewError(fmt.Errorf("invalid guard verdict: %w", buildErr), types.ErrorCodeBadResponseBody)
		}
		results = append(results, result)
		verdicts = append(verdicts, verdict)
		if callUsage != nil {
			addGuardUsage(info, totalUsage, callUsage.PromptTokens, callUsage.CompletionTokens)
		}
	}
	if totalUsage.TotalTokens == 0 {
		// Upstream omitted usage: bill from what was actually sent. The
		// request-level estimate cannot see conversation-form input or the
		// per-call fan-out, so count each guard call's messages plus the
		// guard chat template applied by the serving layer.
		for _, payload := range a.payloads {
			addGuardUsage(info, totalUsage, guardTemplateOverheadTokens, 0)
			for _, message := range payload.Messages {
				addGuardUsage(info, totalUsage, service.CountTextToken(message.Content, info.UpstreamModelName), 0)
			}
		}
		for _, verdict := range verdicts {
			addGuardUsage(info, totalUsage, 0, service.CountTextToken(verdict, info.UpstreamModelName))
		}
	}
	response := &GuardModerationResponse{
		ID:      "guardmod-" + common.GetUUID(),
		Object:  "guard.moderation",
		Created: common.GetTimestamp(),
		Model:   info.OriginModelName,
		Results: results,
		Usage:   totalUsage,
	}
	jsonData, marshalErr := common.Marshal(response)
	if marshalErr != nil {
		return nil, types.NewError(marshalErr, types.ErrorCodeBadResponseBody)
	}
	c.Data(http.StatusOK, "application/json", jsonData)
	return totalUsage, nil
}

// Guard calls fan out, so both reported counts and their accumulation must be
// bounded before normal relay billing receives them.
func addGuardUsage(info *relaycommon.RelayInfo, total *dto.Usage, input, output int) {
	for _, count := range []struct {
		target *int
		value  int
	}{
		{&total.PromptTokens, max(input, 0)},
		{&total.CompletionTokens, max(output, 0)},
	} {
		quota, clamp := common.QuotaFromFloatChecked(float64(*count.target) + float64(count.value))
		*count.target = quota
		if info.QuotaClamp == nil && clamp != nil {
			info.QuotaClamp = clamp
		}
	}
	quota, clamp := common.QuotaFromFloatChecked(float64(total.PromptTokens) + float64(total.CompletionTokens))
	total.TotalTokens = quota
	if info.QuotaClamp == nil && clamp != nil {
		info.QuotaClamp = clamp
	}
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
