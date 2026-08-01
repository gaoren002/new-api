package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	promptAuditPreviewRunes             = 96
	promptAuditFullRunes                = 65536
	promptAuditPreviousOutputTrimMarker = "\n...[previous output truncated]...\n"
)

var (
	promptAuditBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+\-/]+=*`)
	promptAuditAPIKeyPattern = regexp.MustCompile(`(?i)\b(sk|rk|pk|api[_-]?key|token|secret|password)[-_:=\s]+[A-Za-z0-9._~+\-/]{8,}`)
	promptAuditCanaryPattern = regexp.MustCompile(`(?i)([A-Z]+_CANARY_)[A-Za-z0-9_-]+`)
	promptAuditEmailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	promptAuditPhonePattern  = regexp.MustCompile(`(?:\+?\d[\d\s().-]{8,}\d)`)
)

type PromptAuditRequest struct {
	RequestID string
	UserID    int
	Username  string
	UserEmail string
	TokenID   int
	TokenName string
	Group     string
	Provider  string
	Endpoint  string
	Protocol  string
	Model     string
	Body      []byte
	Stage     string
}

type PromptAuditSnapshot struct {
	RequestID       string `json:"request_id"`
	UserID          int    `json:"user_id"`
	Username        string `json:"username"`
	UserEmail       string `json:"user_email"`
	TokenID         int    `json:"token_id"`
	TokenName       string `json:"token_name"`
	Group           string `json:"group"`
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Protocol        string `json:"protocol"`
	Model           string `json:"model"`
	PromptHash      string `json:"prompt_hash"`
	RedactedPreview string `json:"redacted_preview"`
	FullPrompt      string `json:"full_prompt"`
	PromptLength    int    `json:"prompt_length"`
	MessageCount    int    `json:"message_count"`
	Stage           string `json:"stage"`
	ScanText        string `json:"-"`
}

func ExtractPromptAuditSnapshot(request PromptAuditRequest, latestTurnOnly bool, previousOutputLimit int) (PromptAuditSnapshot, error) {
	var document any
	if err := json.Unmarshal(request.Body, &document); err != nil {
		return PromptAuditSnapshot{}, errors.New("prompt audit request JSON is invalid")
	}
	segments := extractPromptAuditProtocolSegments(request.Protocol, document)
	selected := promptAuditFullSegments(segments)
	if latestTurnOnly {
		current, previous := selectPromptAuditTurns(segments)
		if current != "" {
			selected = []string{current}
			if previous != "" {
				selected = append(selected, promptAuditLimitPreviousOutput(previous, previousOutputLimit))
			}
		}
	}
	if len(selected) == 0 {
		return PromptAuditSnapshot{}, ErrNoPromptAuditText
	}
	metadataText := strings.Join(selected, "\n\n")
	scanText := metadataText
	if len(selected) > 1 {
		scanText = selected[0] + promptAuditPrioritySeparator + strings.Join(selected[1:], "\n\n")
	}
	digest := sha256.Sum256([]byte(metadataText))
	stage := strings.TrimSpace(request.Stage)
	if stage == "" {
		stage = "http"
	}
	return PromptAuditSnapshot{
		RequestID: request.RequestID, UserID: request.UserID, Username: request.Username, UserEmail: request.UserEmail,
		TokenID: request.TokenID, TokenName: request.TokenName, Group: request.Group,
		Provider: request.Provider, Endpoint: request.Endpoint, Protocol: request.Protocol, Model: request.Model,
		PromptHash: hex.EncodeToString(digest[:]), RedactedPreview: promptAuditPreview(metadataText),
		FullPrompt:   promptAuditTrimRunes(strings.ReplaceAll(metadataText, "\x00", ""), promptAuditFullRunes),
		PromptLength: utf8.RuneCountInString(metadataText), MessageCount: len(selected), Stage: stage, ScanText: scanText,
	}, nil
}

func promptAuditFullSegments(values []promptAuditSegment) []string {
	segments := make([]promptAuditSegment, 0, len(values))
	for _, segment := range values {
		segment.text = strings.TrimSpace(segment.text)
		if segment.text != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return nil
	}
	priority := len(segments) - 1
	for index := len(segments) - 1; index >= 0; index-- {
		if segments[index].role == "user" {
			priority = index
			break
		}
	}
	result := make([]string, 0, len(segments))
	result = append(result, segments[priority].text)
	for index, segment := range segments {
		if index != priority {
			result = append(result, segment.text)
		}
	}
	return result
}

func promptAuditPreview(value string) string {
	value = redactPromptAuditValue(value, promptAuditPreviewRunes)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) < 32 {
		return "***"
	}
	keep := len(runes) / 4
	if keep > 24 {
		keep = 24
	}
	return string(runes[:keep]) + "***..."
}

func redactPromptAuditValue(value string, maxRunes int) string {
	value = promptAuditBearerPattern.ReplaceAllString(value, "Bearer ***")
	value = promptAuditAPIKeyPattern.ReplaceAllStringFunc(value, func(match string) string {
		if index := strings.IndexAny(match, ":= \t"); index >= 0 {
			return match[:index+1] + "***"
		}
		return "***"
	})
	value = promptAuditCanaryPattern.ReplaceAllString(value, "${1}***")
	value = promptAuditEmailPattern.ReplaceAllString(value, "***@***")
	value = promptAuditPhonePattern.ReplaceAllString(value, "***PHONE***")
	return promptAuditTrimRunes(value, maxRunes)
}

func promptAuditTrimRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func promptAuditLimitPreviousOutput(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	marker := []rune(promptAuditPreviousOutputTrimMarker)
	if limit <= len(marker) {
		return string(runes[:limit])
	}
	remaining := limit - len(marker)
	head := (remaining + 1) / 2
	tail := remaining - head
	return string(runes[:head]) + string(marker) + string(runes[len(runes)-tail:])
}

func PromptAuditFullPromptFromScanText(value string) string {
	return promptAuditTrimRunes(strings.ReplaceAll(value, promptAuditPrioritySeparator, "\n\n"), promptAuditFullRunes)
}
