package qwen3guard

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeModerationInputAcceptedShapes(t *testing.T) {
	t.Run("single string", func(t *testing.T) {
		targets, err := normalizeModerationInput("how do I bake bread")
		require.NoError(t, err)
		require.Len(t, targets, 1)
		assert.Equal(t, []guardChatMessage{{Role: "user", Content: "how do I bake bread"}}, targets[0])
	})

	t.Run("array of strings preserves order with one target each", func(t *testing.T) {
		targets, err := normalizeModerationInput([]any{"first", "second", "third"})
		require.NoError(t, err)
		require.Len(t, targets, 3)
		assert.Equal(t, []guardChatMessage{{Role: "user", Content: "first"}}, targets[0])
		assert.Equal(t, []guardChatMessage{{Role: "user", Content: "second"}}, targets[1])
		assert.Equal(t, []guardChatMessage{{Role: "user", Content: "third"}}, targets[2])
	})

	t.Run("array of messages becomes one conversation target", func(t *testing.T) {
		targets, err := normalizeModerationInput([]any{
			map[string]any{"role": "system", "content": "be helpful"},
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": "hi there"},
		})
		require.NoError(t, err)
		require.Len(t, targets, 1)
		assert.Equal(t, []guardChatMessage{
			{Role: "system", Content: "be helpful"},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		}, targets[0])
	})
}

func TestNormalizeModerationInputRejections(t *testing.T) {
	tooManyStrings := make([]any, MaxModerationInputItems+1)
	for index := range tooManyStrings {
		tooManyStrings[index] = "item"
	}
	tooManyMessages := make([]any, MaxModerationMessages+1)
	for index := range tooManyMessages {
		tooManyMessages[index] = map[string]any{"role": "user", "content": "turn"}
	}
	tests := []struct {
		name  string
		input any
	}{
		{name: "empty string", input: ""},
		{name: "whitespace-only string", input: "   "},
		{name: "empty array", input: []any{}},
		{name: "too many strings", input: tooManyStrings},
		{name: "too many messages", input: tooManyMessages},
		{name: "string then object", input: []any{"text", map[string]any{"role": "user", "content": "hi"}}},
		{name: "object then string", input: []any{map[string]any{"role": "user", "content": "hi"}, "text"}},
		{name: "invalid role", input: []any{map[string]any{"role": "tool", "content": "hi"}}},
		{name: "non-string content", input: []any{map[string]any{"role": "user", "content": 42}}},
		{name: "empty content", input: []any{map[string]any{"role": "user", "content": "  "}}},
		{name: "final message is system", input: []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "system", "content": "be helpful"},
		}},
		{name: "number input", input: 42.0},
		{name: "nil input", input: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeModerationInput(tt.input)
			assert.Error(t, err)
		})
	}
}

func TestBuildGuardChatPayloads(t *testing.T) {
	targets := [][]guardChatMessage{
		{{Role: "user", Content: "first"}},
		{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}},
	}
	payloads := buildGuardChatPayloads("qwen3guard-gen-4b", targets)
	require.Len(t, payloads, 2)
	for index, payload := range payloads {
		assert.Equal(t, "qwen3guard-gen-4b", payload.Model)
		assert.Equal(t, targets[index], payload.Messages)
		assert.Equal(t, 0.0, payload.Temperature)
		assert.Equal(t, 64, payload.MaxTokens)
		assert.Equal(t, 42, payload.Seed)
	}
}

func TestExtractGuardChatContent(t *testing.T) {
	t.Run("string content with usage passthrough", func(t *testing.T) {
		body := []byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
		content, usage, err := extractGuardChatContent(body)
		require.NoError(t, err)
		assert.Equal(t, "Safety: Safe\nCategories: None", content)
		require.NotNil(t, usage)
		assert.Equal(t, 11, usage.PromptTokens)
		assert.Equal(t, 7, usage.CompletionTokens)
		assert.Equal(t, 18, usage.TotalTokens)
	})

	t.Run("text parts joined with newline", func(t *testing.T) {
		body := []byte(`{"choices":[{"message":{"content":[{"text":"Safety: Safe"},{"text":"Categories: None"}]}}]}`)
		content, usage, err := extractGuardChatContent(body)
		require.NoError(t, err)
		assert.Equal(t, "Safety: Safe\nCategories: None", content)
		assert.Nil(t, usage)
	})

	t.Run("no choices", func(t *testing.T) {
		_, _, err := extractGuardChatContent([]byte(`{"choices":[]}`))
		assert.Error(t, err)
	})

	t.Run("invalid json", func(t *testing.T) {
		_, _, err := extractGuardChatContent([]byte(`{"choices":`))
		assert.Error(t, err)
	})

	t.Run("unsupported content shape", func(t *testing.T) {
		_, _, err := extractGuardChatContent([]byte(`{"choices":[{"message":{"content":42}}]}`))
		assert.Error(t, err)
	})
}

func TestBuildModerationResult(t *testing.T) {
	requireCanonicalCategoryKeys := func(t *testing.T, result GuardModerationResult) {
		require.Len(t, result.Categories, len(setting.PromptAuditScannerIDs))
		require.Len(t, result.CategoryScores, len(setting.PromptAuditScannerIDs))
		for _, id := range setting.PromptAuditScannerIDs {
			require.Contains(t, result.Categories, id)
			require.Contains(t, result.CategoryScores, id)
		}
	}

	t.Run("safe verdict", func(t *testing.T) {
		result, err := buildModerationResult(0, "Safety: Safe\nCategories: None")
		require.NoError(t, err)
		assert.Equal(t, 0, result.Index)
		assert.False(t, result.Flagged)
		assert.Equal(t, RiskLevelSafe, result.RiskLevel)
		assert.False(t, result.Refusal)
		requireCanonicalCategoryKeys(t, result)
		for _, id := range setting.PromptAuditScannerIDs {
			assert.False(t, result.Categories[id])
			assert.Equal(t, 0.0, result.CategoryScores[id])
		}
	})

	t.Run("unsafe verdict flags category with score 1", func(t *testing.T) {
		result, err := buildModerationResult(2, "Safety: Unsafe\nCategories: Violent")
		require.NoError(t, err)
		assert.Equal(t, 2, result.Index)
		assert.True(t, result.Flagged)
		assert.Equal(t, RiskLevelUnsafe, result.RiskLevel)
		requireCanonicalCategoryKeys(t, result)
		for _, id := range setting.PromptAuditScannerIDs {
			if id == "violent" {
				assert.True(t, result.Categories[id])
				assert.Equal(t, 1.0, result.CategoryScores[id])
				continue
			}
			assert.False(t, result.Categories[id])
			assert.Equal(t, 0.0, result.CategoryScores[id])
		}
	})

	t.Run("controversial verdict scores 0.5 without flagging", func(t *testing.T) {
		result, err := buildModerationResult(0, "Safety: Controversial\nCategories: PII")
		require.NoError(t, err)
		assert.False(t, result.Flagged)
		assert.Equal(t, RiskLevelControversial, result.RiskLevel)
		requireCanonicalCategoryKeys(t, result)
		assert.True(t, result.Categories["pii"])
		assert.Equal(t, 0.5, result.CategoryScores["pii"])
		for _, id := range setting.PromptAuditScannerIDs {
			if id == "pii" {
				continue
			}
			assert.False(t, result.Categories[id])
			assert.Equal(t, 0.0, result.CategoryScores[id])
		}
	})

	t.Run("refusal yes", func(t *testing.T) {
		result, err := buildModerationResult(0, "Safety: Safe\nCategories: None\nRefusal: Yes")
		require.NoError(t, err)
		assert.True(t, result.Refusal)
	})

	t.Run("refusal no", func(t *testing.T) {
		result, err := buildModerationResult(0, "Safety: Safe\nCategories: None\nRefusal: No")
		require.NoError(t, err)
		assert.False(t, result.Refusal)
	})

	t.Run("invalid verdict without safety line", func(t *testing.T) {
		_, err := buildModerationResult(0, "Categories: None")
		assert.Error(t, err)
	})
}
