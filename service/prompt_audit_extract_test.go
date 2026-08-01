package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractPromptAuditTextUsesLatestUserAndPreviousOutput(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"old user"},
			{"role":"assistant","content":[{"type":"text","text":"previous output part one"},{"type":"text","text":"previous output part two"}]},
			{"role":"tool","content":"tool result"},
			{"role":"user","content":[{"type":"text","text":"current part one"},{"type":"input_text","text":"current part two"}]}
		]
	}`)
	text, err := ExtractPromptAuditText("openai_chat_completions", body)
	require.NoError(t, err)
	require.Equal(t, "current part one\n\ncurrent part two"+promptAuditPrioritySeparator+"previous output part one\n\nprevious output part two", text)
	require.NotContains(t, text, "old user")
	require.NotContains(t, text, "tool result")
}

func TestExtractPromptAuditTextProtocols(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
	}{
		{
			name: "responses", protocol: "openai_responses",
			body: `{"input":[{"role":"assistant","content":[{"type":"output_text","text":"last answer"}]},{"role":"user","content":[{"type":"input_text","text":"new question"}]}]}`,
			want: "new question" + promptAuditPrioritySeparator + "last answer",
		},
		{
			name: "anthropic", protocol: "anthropic_messages",
			body: `{"messages":[{"role":"assistant","content":[{"type":"text","text":"last answer"}]},{"role":"user","content":[{"type":"text","text":"new question"}]}]}`,
			want: "new question" + promptAuditPrioritySeparator + "last answer",
		},
		{
			name: "gemini", protocol: "gemini",
			body: `{"contents":[{"role":"model","parts":[{"text":"last answer"}]},{"role":"user","parts":[{"text":"new question"}]}]}`,
			want: "new question" + promptAuditPrioritySeparator + "last answer",
		},
		{
			name: "images", protocol: "openai_images",
			body: `{"prompt":"draw a red house","image":"data:image/png;base64,AAAA"}`,
			want: "draw a red house",
		},
		{
			name: "embeddings", protocol: "openai_embeddings",
			body: `{"input":["first document","second document"]}`,
			want: "first document\n\nsecond document",
		},
		{
			name: "realtime item", protocol: "openai_realtime",
			body: `{"type":"conversation.item.create","item":{"role":"user","content":[{"type":"input_text","text":"live question"},{"type":"input_audio","transcript":"spoken input"}]}}`,
			want: "live question\n\nspoken input",
		},
		{
			name: "midjourney content", protocol: "media",
			body: `{"content":"change the image prompt","maskBase64":"AAAA"}`,
			want: "change the image prompt",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text, err := ExtractPromptAuditText(test.protocol, []byte(test.body))
			require.NoError(t, err)
			require.Equal(t, test.want, text)
		})
	}
}

func TestExtractPromptAuditTextRequiresCurrentUserInput(t *testing.T) {
	_, err := ExtractPromptAuditText("openai_chat_completions", []byte(`{"messages":[{"role":"system","content":"system only"}]}`))
	require.ErrorIs(t, err, ErrNoPromptAuditText)
	_, err = ExtractPromptAuditText("openai_chat_completions", []byte(`not-json`))
	require.Error(t, err)
}

func TestExtractPromptAuditSnapshotUsesNarrowBlockingAndFullAsyncScopes(t *testing.T) {
	request := PromptAuditRequest{
		RequestID: "req-1", UserID: 12, Username: "alice", UserEmail: "alice@example.com",
		Protocol: "openai_chat_completions", Stage: "http",
		Body: []byte(`{"messages":[{"role":"system","content":"current instruction"},{"role":"user","content":"old input"},{"role":"assistant","content":"previous output"},{"role":"user","content":"latest input"}]}`),
	}
	blocking, err := ExtractPromptAuditSnapshot(request, true, 8000)
	require.NoError(t, err)
	require.Contains(t, blocking.ScanText, "latest input")
	require.Contains(t, blocking.ScanText, "previous output")
	require.NotContains(t, blocking.ScanText, "old input")
	require.NotContains(t, blocking.ScanText, "current instruction")

	async, err := ExtractPromptAuditSnapshot(request, false, 8000)
	require.NoError(t, err)
	require.Contains(t, async.ScanText, "latest input")
	require.Contains(t, async.ScanText, "old input")
	require.Contains(t, async.ScanText, "current instruction")
	require.Equal(t, "alice@example.com", async.UserEmail)
	require.NotEmpty(t, async.PromptHash)
}

func TestExtractPromptAuditSnapshotLimitsPreviousOutputOnly(t *testing.T) {
	current := strings.Repeat("current", 20)
	previous := strings.Repeat("A", 40) + "middle" + strings.Repeat("Z", 40)
	body := []byte(`{"messages":[{"role":"assistant","content":"` + previous + `"},{"role":"user","content":"` + current + `"}]}`)

	snapshot, err := ExtractPromptAuditSnapshot(PromptAuditRequest{
		Protocol: "openai_chat_completions",
		Body:     body,
	}, true, 48)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(snapshot.ScanText, current+promptAuditPrioritySeparator))
	require.Contains(t, snapshot.ScanText, promptAuditPreviousOutputTrimMarker)
	require.NotContains(t, snapshot.ScanText, "middle")
	previousScan := strings.SplitN(snapshot.ScanText, promptAuditPrioritySeparator, 2)[1]
	require.Len(t, []rune(previousScan), 48)
	require.Contains(t, previousScan, "AAAA")
	require.Contains(t, previousScan, "ZZZZ")
}

func TestExtractPromptAuditTextSupportsCompletionsAndAlphaSearch(t *testing.T) {
	completion, err := ExtractPromptAuditText("openai_chat_completions", []byte(`{"prompt":"complete this sentence"}`))
	require.NoError(t, err)
	require.Equal(t, "complete this sentence", completion)
	search, err := ExtractPromptAuditText("openai_alpha_search", []byte(`{"query":"latest release notes","model":"gpt"}`))
	require.NoError(t, err)
	require.Equal(t, "latest release notes", search)
}
