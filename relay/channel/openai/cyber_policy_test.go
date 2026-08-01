package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func newRealtimeWebsocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConn := make(chan *websocket.Conn, 1)
	upgradeErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			upgradeErr <- err
			return
		}
		serverConn <- conn
	}))
	t.Cleanup(server.Close)

	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	var handlerConn *websocket.Conn
	select {
	case handlerConn = <-serverConn:
	case err = <-upgradeErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out creating websocket pair")
	}
	t.Cleanup(func() {
		_ = peer.Close()
		if handlerConn != nil {
			_ = handlerConn.Close()
		}
	})
	return handlerConn, peer
}

func TestResponsesStreamForwardsAndMarksCyberPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6"},
	}
	event := `{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"blocked upstream"},"usage":{"input_tokens":1234,"output_tokens":7}}}`
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + event + "\n\n")),
	}
	usage, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.Equal(t, 1234, usage.PromptTokens)
	require.Equal(t, 7, usage.CompletionTokens)
	require.Equal(t, 1241, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), "cyber_policy")
	mark := service.GetCyberPolicyMark(c)
	require.NotNil(t, mark)
	require.Equal(t, "blocked upstream", mark.Message)
}

func TestCyberPolicyHTTPErrorPreservesReportedUsage(t *testing.T) {
	for _, payload := range []string{
		`{"error":{"code":"cyber_policy","message":"blocked upstream"},"usage":{"prompt_tokens":41,"completion_tokens":3}}`,
		`{"response":{"error":{"code":"cyber_policy","message":"blocked upstream"},"usage":{"input_tokens":41,"output_tokens":3}}}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		apiErr := service.RelayErrorHandler(c, &http.Response{
			StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(payload)),
		}, false)
		require.NotNil(t, apiErr)
		require.Equal(t, types.ErrorCodeCyberPolicy, apiErr.GetErrorCode())
		// The controller's fallback marker must not replace the parsed usage.
		service.MarkCyberPolicy(c, service.CyberPolicyMark{Message: apiErr.Error(), UpstreamStatus: apiErr.StatusCode})
		mark := service.GetCyberPolicyMark(c)
		require.NotNil(t, mark)
		require.Equal(t, 41, mark.UpstreamInputTokens)
		require.Equal(t, 3, mark.UpstreamOutputTokens)
		require.Equal(t, http.StatusForbidden, mark.UpstreamStatus)
	}
}

func TestResponsesCyberPolicyDoesNotBillPartialOutputWithoutUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "local-counting-model"}}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"some partial output\"}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"cyber_policy\",\"message\":\"blocked\"}}}\n\n")),
	}
	usage, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.NotNil(t, service.GetCyberPolicyMark(c))
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.TotalTokens)
}

func TestRealtimeLocalUsageIsSettledOnlyOnNormalClose(t *testing.T) {
	for _, cyberPolicy := range []bool{false, true} {
		name := "normal close"
		if cyberPolicy {
			name = "cyber rejection without usage"
		}
		t.Run(name, func(t *testing.T) {
			clientConn, clientPeer := newRealtimeWebsocketPair(t)
			targetConn, targetPeer := newRealtimeWebsocketPair(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			info := &relaycommon.RelayInfo{
				ClientWs: clientConn, TargetWs: targetConn, UsePrice: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "local-counting-model"},
			}
			type result struct {
				err   *types.NewAPIError
				usage int
			}
			done := make(chan result, 1)
			go func() {
				apiErr, usage := OpenaiRealtimeHandler(c, info)
				done <- result{err: apiErr, usage: usage.TotalTokens}
			}()
			const delta = "some partial output"
			require.NoError(t, targetPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.audio_transcript.delta","delta":"some partial output"}`)))
			require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(3*time.Second)))
			_, _, err := clientPeer.ReadMessage()
			require.NoError(t, err, "forwarding the delta proves local usage was collected before closing")
			if cyberPolicy {
				require.NoError(t, targetPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"blocked"}}}`)))
			} else {
				require.NoError(t, targetPeer.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(3*time.Second)))
			}
			select {
			case got := <-done:
				require.Nil(t, got.err)
				if cyberPolicy {
					require.Zero(t, got.usage)
				} else {
					require.Positive(t, got.usage)
					require.Equal(t, service.CountTextToken(delta, info.UpstreamModelName), got.usage)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("realtime handler did not finish")
			}
		})
	}
}

func TestResponsesToChatStreamFormatsCyberPolicyForChatClients(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	event := `{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"blocked upstream"}}}`
	c, recorder, response, info := newResponsesChatTestContext(t, "data: "+event+"\n\n", true)
	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Zero(t, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"code":"cyber_policy"`)
	require.Contains(t, recorder.Body.String(), `"type":"invalid_request_error"`)
	require.NotContains(t, recorder.Body.String(), `"type":"response.failed"`)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
	require.NotNil(t, service.GetCyberPolicyMark(c))
}

func TestResponsesToChatStreamFormatsCyberPolicyForClaudeClients(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	event := `{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"blocked upstream"}}}`
	c, recorder, response, info := newResponsesChatTestContext(t, "data: "+event+"\n\n", true)
	info.RelayFormat = types.RelayFormatClaude
	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Zero(t, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), `"type":"permission_error"`)
	require.NotContains(t, recorder.Body.String(), `"type":"response.failed"`)
	require.NotNil(t, service.GetCyberPolicyMark(c))
}

func TestResponsesToChatStreamFormatsCyberPolicyForGeminiClients(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	event := `{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"blocked upstream"}}}`
	c, recorder, response, info := newResponsesChatTestContext(t, "data: "+event+"\n\n", true)
	info.RelayFormat = types.RelayFormatGemini
	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, response)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Zero(t, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"status":"PERMISSION_DENIED"`)
	require.Contains(t, recorder.Body.String(), `"message":"blocked upstream"`)
	require.NotContains(t, recorder.Body.String(), `"type":"response.failed"`)
	require.NotNil(t, service.GetCyberPolicyMark(c))
}

func TestOpenAIChatStreamTerminatesCyberPolicyWithoutBillingUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6"},
		RelayFormat: types.RelayFormatOpenAI,
		DisablePing: true,
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"error\":{\"code\":\"cyber_policy\",\"message\":\"blocked upstream\"},\"usage\":{\"input_tokens\":17,\"output_tokens\":2}}\n\n",
		)),
	}

	usage, apiErr := OaiStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 17, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.Equal(t, 19, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"code":"cyber_policy"`)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
	require.NotNil(t, service.GetCyberPolicyMark(c))
}

func TestOpenAIRealtimeForwardsAndMarksCyberPolicyUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clientConn, clientPeer := newRealtimeWebsocketPair(t)
	targetConn, targetPeer := newRealtimeWebsocketPair(t)
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil).WithContext(requestContext)
	info := &relaycommon.RelayInfo{
		ClientWs:    clientConn,
		TargetWs:    targetConn,
		UsePrice:    true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6"},
	}
	type handlerResult struct {
		err   *types.NewAPIError
		usage int
		in    int
		out   int
	}
	resultChan := make(chan handlerResult, 1)
	go func() {
		apiErr, usage := OpenaiRealtimeHandler(c, info)
		result := handlerResult{err: apiErr}
		if usage != nil {
			result.usage = usage.TotalTokens
			result.in = usage.InputTokens
			result.out = usage.OutputTokens
		}
		resultChan <- result
	}()

	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"blocked upstream"},"usage":{"input_tokens":41,"output_tokens":3}}}`)
	require.NoError(t, targetPeer.WriteMessage(websocket.TextMessage, payload))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(3*time.Second)))
	messageType, forwarded, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, string(payload), string(forwarded))

	select {
	case result := <-resultChan:
		require.Nil(t, result.err)
		require.Equal(t, 44, result.usage)
		require.Equal(t, 41, result.in)
		require.Equal(t, 3, result.out)
	case <-time.After(3 * time.Second):
		t.Fatal("realtime handler did not stop after cyber_policy")
	}
	mark := service.GetCyberPolicyMark(c)
	require.NotNil(t, mark)
	require.Equal(t, "blocked upstream", mark.Message)
	require.Equal(t, 41, mark.UpstreamInputTokens)
	require.Equal(t, 3, mark.UpstreamOutputTokens)
}
