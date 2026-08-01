package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestEvaluatePromptAuditCallsGuardAndBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/chat/completions", request.URL.Path)
		require.Equal(t, "Bearer audit-key", request.Header.Get("Authorization"))
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "guard-model", payload.Model)
		require.Equal(t, "current input", payload.Messages[0].Content)
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Unsafe\nCategories: Jailbreak"}}]}`))
	}))
	defer server.Close()

	decision, err := evaluatePromptAudit(context.Background(), promptAuditTestConfig(server.URL), "openai_chat_completions", []byte(`{"messages":[{"role":"user","content":"current input"}]}`), server.Client())
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, []string{"jailbreak"}, decision.MatchedScanners)
}

func TestEvaluatePromptAuditStopsAfterBlockingChunk(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		content := "Safety: Unsafe\nCategories: Jailbreak"
		if call == 1 {
			content = "Safety: Safe\nCategories: None"
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":` + mustJSONPromptAudit(t, content) + `}}]}`))
	}))
	defer server.Close()
	config := promptAuditTestConfig(server.URL)
	config.InputLimit = 4
	body := []byte(`{"messages":[{"role":"assistant","content":"previous output"},{"role":"user","content":"abcdefghijk"}]}`)
	decision, err := evaluatePromptAudit(context.Background(), config, "openai_chat_completions", body, server.Client())
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, int32(2), calls.Load())
}

func TestParseQwenGuardPromptAuditHonorsScannerSelection(t *testing.T) {
	decision, err := ParseQwenGuardPromptAudit("Safety: Unsafe\nCategories: Jailbreak", []string{"pii"})
	require.NoError(t, err)
	require.False(t, decision.Blocked)
	decision, err = ParseQwenGuardPromptAudit("Safety: Controversial\nCategories: PII", []string{"pii"})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	decision, err = ParseQwenGuardPromptAudit("Safety: Unsafe\nCategories: Future Risk", []string{"pii"})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Len(t, decision.UnknownCategories, 1)
	require.True(t, strings.HasPrefix(decision.UnknownCategories[0], "unknown:"))
	require.NotContains(t, decision.UnknownCategories[0], "future")
	_, err = ParseQwenGuardPromptAudit("not a guard response", []string{"pii"})
	require.Error(t, err)
}

func TestPromptAuditEndpointFailover(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer second.Close()
	config := promptAuditTestConfig(first.URL)
	config.Endpoints = []setting.PromptAuditEndpoint{
		{ID: "first", Name: "First", Protocol: "openai_compatible", BaseURL: first.URL, Model: "guard-model", TimeoutMS: 1000, InputLimit: 8000, Enabled: true},
		{ID: "second", Name: "Second", Protocol: "openai_compatible", BaseURL: second.URL, Model: "guard-model", TimeoutMS: 1000, InputLimit: 8000, Enabled: true},
	}
	before := promptAuditRuntime.metrics.failovers.Load()
	decision, err := scanPromptAuditChunkWithFailover(context.Background(), config, "hello", second.Client())
	require.NoError(t, err)
	require.Equal(t, "second", decision.GuardEndpointID)
	require.Equal(t, before+1, promptAuditRuntime.metrics.failovers.Load())
}

func TestPromptAuditClientDoesNotFollowRedirectWithToken(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", target.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	endpoint := setting.PromptAuditEndpoint{ID: "redirect", BaseURL: redirect.URL, TimeoutMS: 1000}
	client, err := promptAuditHTTPClient(endpoint)
	require.NoError(t, err)
	config := promptAuditTestConfig(redirect.URL)
	_, err = scanPromptAuditChunk(context.Background(), config, "hello", client)
	require.Error(t, err)
	require.False(t, leaked.Load())
}

func TestPromptAuditDeleteConfirmationBindingAndExpiryShape(t *testing.T) {
	token, expiresAt, err := NewPromptAuditDeleteConfirmation(7, strings.Repeat("a", 64), 99)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))
	require.NoError(t, ValidatePromptAuditDeleteConfirmation(token, 7, strings.Repeat("a", 64), 99))
	require.Error(t, ValidatePromptAuditDeleteConfirmation(token, 8, strings.Repeat("a", 64), 99))
	require.Error(t, ValidatePromptAuditDeleteConfirmation(token, 7, strings.Repeat("b", 64), 99))
}

func TestPromptAuditChatCompletionsURL(t *testing.T) {
	tests := map[string]string{
		"https://guard.example":                     "https://guard.example/v1/chat/completions",
		"https://guard.example/openai/v1":           "https://guard.example/openai/v1/chat/completions",
		"https://guard.example/v1/chat/completions": "https://guard.example/v1/chat/completions",
	}
	for input, expected := range tests {
		actual, err := PromptAuditChatCompletionsURL(input)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func TestPromptAuditDoesNotLimitConcurrentCalls(t *testing.T) {
	const requestCount = 80
	var started atomic.Int32
	allStarted := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if started.Add(1) == requestCount {
			close(allStarted)
		}
		<-release
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer server.Close()

	config := promptAuditTestConfig(server.URL)
	config.TimeoutMS = 5000
	results := make(chan error, requestCount)
	for index := 0; index < requestCount; index++ {
		go func() {
			_, err := evaluatePromptAudit(
				context.Background(),
				config,
				"openai_chat_completions",
				[]byte(`{"messages":[{"role":"user","content":"benign input"}]}`),
				server.Client(),
			)
			results <- err
		}()
	}

	select {
	case <-allStarted:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatalf("only %d of %d calls reached the endpoint", started.Load(), requestCount)
	}
	close(release)
	for index := 0; index < requestCount; index++ {
		require.NoError(t, <-results)
	}
}

func promptAuditTestConfig(baseURL string) setting.PromptAuditConfig {
	return setting.PromptAuditConfig{
		Enabled: true, BaseURL: baseURL, Model: "guard-model", APIKey: "audit-key",
		TimeoutMS: 1000, InputLimit: 8000, MaxConcurrency: 4,
		Scanners: append([]string(nil), setting.PromptAuditScannerIDs...),
	}
}

func mustJSONPromptAudit(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
