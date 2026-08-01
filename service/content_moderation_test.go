package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func contentModerationTestConfig(t *testing.T, baseURL string, keys ...string) setting.ContentModerationStorageConfig {
	t.Helper()
	t.Setenv(setting.PromptAuditEncryptionKeyEnv, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	config := setting.DefaultContentModerationStorageConfig()
	config.Enabled = true
	config.Mode = setting.ContentModerationModePreBlock
	config.BaseURL = baseURL
	config.RetryCount = len(keys)
	for _, key := range keys {
		ciphertext, err := setting.EncryptContentModerationAPIKey(key)
		require.NoError(t, err)
		config.APIKeyCiphertexts = append(config.APIKeyCiphertexts, ciphertext)
	}
	return config
}

func resetContentModerationKeyRuntime() {
	contentModerationRuntime.cursor.Store(0)
	contentModerationRuntime.healthMu.Lock()
	contentModerationRuntime.health = make(map[string]*contentModerationKeyHealth)
	contentModerationRuntime.healthMu.Unlock()
}

func TestCallContentModerationUsesExpectedEndpointAndThresholds(t *testing.T) {
	resetContentModerationKeyRuntime()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/moderations", request.URL.Path)
		require.Equal(t, "Bearer audit-key", request.Header.Get("Authorization"))
		var payload moderationAPIRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, setting.DefaultContentModerationModel, payload.Model)
		_, _ = writer.Write([]byte(`{"results":[{"flagged":false,"category_scores":{"hate":0.75,"violence":0.1}}]}`))
	}))
	defer server.Close()

	config := contentModerationTestConfig(t, server.URL, "audit-key")
	result, err := callContentModeration(context.Background(), config, "test input")
	require.NoError(t, err)
	flagged, category, score := evaluateContentModerationScores(result.CategoryScores, config.Thresholds)
	require.True(t, flagged)
	require.Equal(t, "hate", category)
	require.Equal(t, 0.75, score)
}

func TestCallContentModerationRetriesWithNextKey(t *testing.T) {
	resetContentModerationKeyRuntime()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Header.Get("Authorization") == "Bearer first-key" {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(`{"results":[{"category_scores":{"hate":0.1}}]}`))
	}))
	defer server.Close()

	config := contentModerationTestConfig(t, server.URL, "first-key", "second-key")
	result, err := callContentModeration(context.Background(), config, "test input")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int32(2), calls.Load())
	statuses := ContentModerationAPIKeyStatuses(config)
	require.Equal(t, "frozen", statuses[0].Status)
	require.Equal(t, "ready", statuses[1].Status)
	require.Equal(t, int64(0), statuses[0].Active)
	require.Equal(t, int64(1), statuses[0].TotalCalls)
	require.Equal(t, int64(1), statuses[0].ErrorCount)
	require.Equal(t, int64(1), statuses[1].TotalCalls)
}

func TestContentModerationUsesConfiguredProxy(t *testing.T) {
	resetContentModerationKeyRuntime()
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, "moderation.invalid", request.URL.Host)
		require.Equal(t, "Bearer audit-key", request.Header.Get("Authorization"))
		_, _ = writer.Write([]byte(`{"results":[{"category_scores":{"hate":0.1}}]}`))
	}))
	defer proxy.Close()

	config := contentModerationTestConfig(t, "http://moderation.invalid", "audit-key")
	config.ProxyURL = proxy.URL
	result, err := callContentModeration(context.Background(), config, "test input")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int32(1), calls.Load())
}

func TestContentModerationClientDoesNotFollowRedirectWithKey(t *testing.T) {
	resetContentModerationKeyRuntime()
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

	config := contentModerationTestConfig(t, redirect.URL, "audit-key")
	_, err := callContentModeration(context.Background(), config, "test input")
	require.Error(t, err)
	require.False(t, leaked.Load())
}

func TestContentModerationEndpointSupportsRootAndV1(t *testing.T) {
	tests := map[string]string{
		"https://example.com":                "https://example.com/v1/moderations",
		"https://example.com/openai/v1":      "https://example.com/openai/v1/moderations",
		"https://example.com/v1/moderations": "https://example.com/v1/moderations",
	}
	for input, expected := range tests {
		actual, err := contentModerationEndpoint(input)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
}

func TestValidateContentModerationTestImages(t *testing.T) {
	require.NoError(t, validateContentModerationTestImages(nil))
	require.NoError(t, validateContentModerationTestImages([]string{"https://example.com/image.png"}))
	require.Error(t, validateContentModerationTestImages([]string{"https://example.com/one.png", "https://example.com/two.png"}))
	require.Error(t, validateContentModerationTestImages([]string{"data:image/png;base64,not-valid"}))
}

func TestContentModerationPreBlockDoesNotAddGlobalConcurrencyLimit(t *testing.T) {
	resetContentModerationKeyRuntime()
	const requestCount = 40
	var started atomic.Int32
	allStarted := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if started.Add(1) == requestCount {
			close(allStarted)
		}
		<-release
		_, _ = writer.Write([]byte(`{"results":[{"category_scores":{"hate":0.1}}]}`))
	}))
	defer server.Close()
	config := contentModerationTestConfig(t, server.URL, "audit-key")
	config.TimeoutMS = 5000
	results := make(chan error, requestCount)
	for index := 0; index < requestCount; index++ {
		go func() {
			_, err := callContentModeration(context.Background(), config, "test input")
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
