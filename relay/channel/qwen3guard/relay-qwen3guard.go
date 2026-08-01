package qwen3guard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
)

const (
	// MaxModerationInputItems bounds the upstream fan-out per request: each
	// input item costs one guard call, so the count must stay bounded before
	// it reaches billing (see AGENTS.md billing safety invariants).
	MaxModerationInputItems = 32
	// MaxModerationMessages bounds a single conversation target.
	MaxModerationMessages = 64
	// maxGuardResponseBytes caps a single upstream guard response body.
	maxGuardResponseBytes = 256 * 1024

	guardTemperature = 0
	guardMaxTokens   = 64
	guardSeed        = 42

	// guardTemplateOverheadTokens approximates the guard chat template the
	// serving layer wraps around each call (safety policy and category
	// definitions), used only when upstream omits usage.
	guardTemplateOverheadTokens = 300
)

// RiskLevel values exposed by the moderation response.
const (
	RiskLevelSafe          = "safe"
	RiskLevelControversial = "controversial"
	RiskLevelUnsafe        = "unsafe"
)

var guardAllowedRoles = map[string]struct{}{
	"system":    {},
	"user":      {},
	"assistant": {},
}

// normalizeModerationInput converts the request `input` field into guard
// targets. Accepted shapes:
//   - a single string: one prompt check
//   - an array of strings: independent prompt checks, one result per item
//   - an array of {role, content} messages: one conversation check that
//     classifies the final turn with the preceding turns as context
func normalizeModerationInput(input any) ([][]guardChatMessage, error) {
	switch value := input.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, errors.New("input must not be empty")
		}
		return [][]guardChatMessage{{{Role: "user", Content: value}}}, nil
	case []any:
		if len(value) == 0 {
			return nil, errors.New("input must not be empty")
		}
		if _, ok := value[0].(string); ok {
			return normalizeModerationStrings(value)
		}
		return normalizeModerationConversation(value)
	default:
		return nil, errors.New("input must be a string, an array of strings, or an array of {role, content} messages")
	}
}

func normalizeModerationStrings(items []any) ([][]guardChatMessage, error) {
	if len(items) > MaxModerationInputItems {
		return nil, fmt.Errorf("input accepts at most %d items per request", MaxModerationInputItems)
	}
	targets := make([][]guardChatMessage, 0, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("input[%d] must be a string; strings and message objects cannot be mixed", index)
		}
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("input[%d] must not be empty", index)
		}
		targets = append(targets, []guardChatMessage{{Role: "user", Content: text}})
	}
	return targets, nil
}

func normalizeModerationConversation(items []any) ([][]guardChatMessage, error) {
	if len(items) > MaxModerationMessages {
		return nil, fmt.Errorf("input accepts at most %d conversation messages per request", MaxModerationMessages)
	}
	messages := make([]guardChatMessage, 0, len(items))
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input[%d] must be a {role, content} message; strings and message objects cannot be mixed", index)
		}
		role, _ := entry["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		if _, allowed := guardAllowedRoles[role]; !allowed {
			return nil, fmt.Errorf("input[%d].role must be one of system, user, assistant", index)
		}
		content, ok := entry["content"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("input[%d].content must be a non-empty string", index)
		}
		messages = append(messages, guardChatMessage{Role: role, Content: content})
	}
	last := messages[len(messages)-1]
	if last.Role == "system" {
		return nil, errors.New("the final conversation message must be a user or assistant turn")
	}
	return [][]guardChatMessage{messages}, nil
}

func buildGuardChatPayloads(model string, targets [][]guardChatMessage) []guardChatPayload {
	payloads := make([]guardChatPayload, 0, len(targets))
	for _, target := range targets {
		payloads = append(payloads, guardChatPayload{
			Model:       model,
			Messages:    target,
			Temperature: guardTemperature,
			MaxTokens:   guardMaxTokens,
			Seed:        guardSeed,
		})
	}
	return payloads
}

// extractGuardChatContent unwraps choices[0].message.content from an upstream
// OpenAI-compatible chat completion body. Content may be a plain string or an
// array of {text} parts.
func extractGuardChatContent(body []byte) (string, *dto.Usage, error) {
	var response guardChatResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return "", nil, fmt.Errorf("invalid upstream response: %w", err)
	}
	if len(response.Choices) == 0 {
		return "", nil, errors.New("upstream response has no choices")
	}
	switch content := response.Choices[0].Message.Content.(type) {
	case string:
		return content, response.Usage, nil
	case []any:
		parts := make([]string, 0, len(content))
		for _, part := range content {
			entry, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := entry["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			return "", nil, errors.New("upstream response content is empty")
		}
		return strings.Join(parts, "\n"), response.Usage, nil
	default:
		return "", nil, errors.New("upstream response content has an unsupported shape")
	}
}

// buildModerationResult maps one guard verdict to a moderation result. The
// heavy lifting (Safety/Categories parsing, category alias normalization)
// is shared with the prompt-audit subsystem via service.ParseQwenGuardPromptAudit.
func buildModerationResult(index int, verdict string) (GuardModerationResult, error) {
	decision, err := service.ParseQwenGuardPromptAudit(verdict, setting.PromptAuditScannerIDs)
	if err != nil {
		return GuardModerationResult{}, err
	}
	riskLevel := strings.ToLower(decision.Safety)
	categories := make(map[string]bool, len(setting.PromptAuditScannerIDs))
	scores := make(map[string]float64, len(setting.PromptAuditScannerIDs))
	for _, id := range setting.PromptAuditScannerIDs {
		categories[id] = false
		scores[id] = 0
	}
	score := 0.0
	switch riskLevel {
	case RiskLevelControversial:
		score = 0.5
	case RiskLevelUnsafe:
		score = 1
	}
	for _, id := range decision.Categories {
		categories[id] = true
		scores[id] = score
	}
	return GuardModerationResult{
		Index:          index,
		Flagged:        riskLevel == RiskLevelUnsafe,
		RiskLevel:      riskLevel,
		Refusal:        parseGuardRefusal(verdict),
		Categories:     categories,
		CategoryScores: scores,
	}, nil
}

// parseGuardRefusal reads the optional `Refusal:` line emitted when the
// classified turn is an assistant response.
func parseGuardRefusal(verdict string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(verdict, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "refusal:") {
			return strings.EqualFold(strings.TrimSpace(line[len("refusal:"):]), "yes")
		}
	}
	return false
}
