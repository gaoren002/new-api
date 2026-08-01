package qwen3guard

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// GuardModerationResponse is the response body of the Qwen3Guard moderation
// endpoint (POST /v1/guard/moderations, also served on POST /v1/moderations).
// It is a superset of the OpenAI moderations response: `flagged`,
// `categories` and `category_scores` keep OpenAI semantics, while
// `risk_level` exposes Qwen3Guard's three-tier verdict (safe / controversial
// / unsafe) and `refusal` reports refusal detection for assistant turns.
type GuardModerationResponse struct {
	ID      string                  `json:"id"`
	Object  string                  `json:"object"`
	Created int64                   `json:"created"`
	Model   string                  `json:"model"`
	Results []GuardModerationResult `json:"results"`
	Usage   *dto.Usage              `json:"usage,omitempty"`
}

type GuardModerationResult struct {
	Index          int                `json:"index"`
	Flagged        bool               `json:"flagged"`
	RiskLevel      string             `json:"risk_level"`
	Refusal        bool               `json:"refusal"`
	Categories     map[string]bool    `json:"categories"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

// guardChatMessage / guardChatPayload mirror the upstream OpenAI-compatible
// chat completion call used to query a Qwen3Guard-Gen model. The guard model's
// chat template performs the classification; the payload carries the content
// to classify verbatim.
type guardChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type guardChatPayload struct {
	Model       string             `json:"model"`
	Messages    []guardChatMessage `json:"messages"`
	Temperature float64            `json:"temperature"`
	MaxTokens   int                `json:"max_tokens"`
	Seed        int                `json:"seed"`
}

type guardChatResponseMessage struct {
	Content any `json:"content"`
}

type guardChatResponseChoice struct {
	Message guardChatResponseMessage `json:"message"`
}

type guardChatResponse struct {
	Choices []guardChatResponseChoice `json:"choices"`
	Usage   *dto.Usage                `json:"usage,omitempty"`
}
