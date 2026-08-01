package service

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

var ErrNoPromptAuditText = errors.New("prompt audit request contains no current user text")

const promptAuditPrioritySeparator = "\x00NEW_API_PROMPT_AUDIT_PRIORITY_END\x00"

type promptAuditSegment struct {
	text string
	role string
	turn int
}

func ExtractPromptAuditText(protocol string, body []byte) (string, error) {
	return extractPromptAuditText(protocol, body, 0)
}

func extractPromptAuditText(protocol string, body []byte, previousOutputLimit int) (string, error) {
	var document any
	if err := json.Unmarshal(body, &document); err != nil {
		return "", errors.New("prompt audit request JSON is invalid")
	}
	segments := extractPromptAuditProtocolSegments(protocol, document)
	current, previous := selectPromptAuditTurns(segments)
	if current == "" {
		return "", ErrNoPromptAuditText
	}
	if previous == "" {
		return current, nil
	}
	previous = promptAuditLimitPreviousOutput(previous, previousOutputLimit)
	return current + promptAuditPrioritySeparator + previous, nil
}

func extractPromptAuditProtocolSegments(protocol string, document any) []promptAuditSegment {
	root, _ := document.(map[string]any)
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "openai_chat_completions":
		segments := extractPromptAuditMessages(root["messages"])
		if len(segments) == 0 {
			segments = extractPromptAuditMedia(root)
		}
		return segments
	case "anthropic_messages":
		return append(extractPromptAuditInstruction(root["system"], -1), extractPromptAuditMessages(root["messages"])...)
	case "openai_responses":
		return append(extractPromptAuditInstruction(root["instructions"], -1), extractPromptAuditResponses(root)...)
	case "gemini":
		return append(extractPromptAuditGeminiInstructions(root), extractPromptAuditGemini(root)...)
	case "openai_images", "openai_alpha_search":
		return extractPromptAuditMedia(root)
	case "openai_embeddings", "openai_audio", "rerank", "media":
		return extractPromptAuditMedia(root)
	case "openai_realtime":
		return extractPromptAuditRealtime(root)
	default:
		return nil
	}
}

func extractPromptAuditRealtime(root map[string]any) []promptAuditSegment {
	if root == nil {
		return nil
	}
	switch stringValue(root["type"]) {
	case "session.update":
		session, _ := root["session"].(map[string]any)
		return extractPromptAuditInstruction(session["instructions"], 0)
	case "conversation.item.create":
		item, _ := root["item"].(map[string]any)
		role := normalizePromptAuditRole(stringValue(item["role"]))
		if role == "" {
			role = "user"
		}
		texts := promptAuditContentTexts(item["content"])
		result := make([]promptAuditSegment, 0, len(texts))
		for _, text := range texts {
			result = append(result, promptAuditSegment{text: text, role: role, turn: 0})
		}
		return result
	case "response.create":
		response, _ := root["response"].(map[string]any)
		result := extractPromptAuditInstruction(response["instructions"], 0)
		result = append(result, extractPromptAuditMedia(response)...)
		return result
	default:
		return extractPromptAuditMedia(root)
	}
}

func extractPromptAuditInstruction(value any, turn int) []promptAuditSegment {
	texts := promptAuditContentTexts(value)
	if object, ok := value.(map[string]any); ok && len(texts) == 0 {
		texts = promptAuditContentTexts(object["parts"])
	}
	result := make([]promptAuditSegment, 0, len(texts))
	for _, text := range texts {
		result = append(result, promptAuditSegment{text: text, role: "system", turn: turn})
	}
	return result
}

func extractPromptAuditGeminiInstructions(root map[string]any) []promptAuditSegment {
	if root == nil {
		return nil
	}
	result := extractPromptAuditInstruction(root["systemInstruction"], -2)
	result = append(result, extractPromptAuditInstruction(root["system_instruction"], -1)...)
	return result
}

func extractPromptAuditMessages(value any) []promptAuditSegment {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]promptAuditSegment, 0, len(items))
	for turn, item := range items {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := normalizePromptAuditRole(stringValue(message["role"]))
		if role == "" {
			continue
		}
		for _, text := range promptAuditContentTexts(message["content"]) {
			result = append(result, promptAuditSegment{text: text, role: role, turn: turn})
		}
	}
	return result
}

func extractPromptAuditResponses(root map[string]any) []promptAuditSegment {
	if root == nil {
		return nil
	}
	input := root["input"]
	if frameType := stringValue(root["type"]); frameType != "" {
		if frameType != "response.create" {
			return nil
		}
		if response, ok := root["response"].(map[string]any); ok && input == nil {
			input = response["input"]
		}
	}
	switch typed := input.(type) {
	case string:
		return []promptAuditSegment{{text: typed, role: "user", turn: 0}}
	case map[string]any:
		return extractPromptAuditResponseItem(typed, 0)
	case []any:
		result := make([]promptAuditSegment, 0, len(typed))
		for turn, item := range typed {
			switch entry := item.(type) {
			case string:
				result = append(result, promptAuditSegment{text: entry, role: "user", turn: turn})
			case map[string]any:
				result = append(result, extractPromptAuditResponseItem(entry, turn)...)
			}
		}
		return result
	default:
		return nil
	}
}

func extractPromptAuditResponseItem(item map[string]any, turn int) []promptAuditSegment {
	role := normalizePromptAuditRole(stringValue(item["role"]))
	if role == "" {
		role = "user"
	}
	content, exists := item["content"]
	if !exists {
		content = item["text"]
	}
	texts := promptAuditContentTexts(content)
	result := make([]promptAuditSegment, 0, len(texts))
	for _, text := range texts {
		result = append(result, promptAuditSegment{text: text, role: role, turn: turn})
	}
	return result
}

func extractPromptAuditGemini(root map[string]any) []promptAuditSegment {
	if root == nil {
		return nil
	}
	result := extractPromptAuditGeminiContents(root["contents"], 0)
	result = append(result, extractPromptAuditGeminiContents(root["content"], len(result)+1000)...)
	if requests, ok := root["requests"].([]any); ok {
		for index, item := range requests {
			request, ok := item.(map[string]any)
			if !ok {
				continue
			}
			result = append(result, extractPromptAuditGeminiContents(request["contents"], (index+1)*10000)...)
			result = append(result, extractPromptAuditGeminiContents(request["content"], (index+1)*10000+1000)...)
		}
	}
	if instances, ok := root["instances"].([]any); ok {
		for index, item := range instances {
			instance, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if prompt := stringValue(instance["prompt"]); prompt != "" {
				result = append(result, promptAuditSegment{text: prompt, role: "user", turn: 100000 + index})
			}
		}
	}
	return result
}

func extractPromptAuditGeminiContents(value any, turnOffset int) []promptAuditSegment {
	var contents []any
	switch typed := value.(type) {
	case []any:
		contents = typed
	case map[string]any:
		contents = []any{typed}
	default:
		return nil
	}
	result := make([]promptAuditSegment, 0, len(contents))
	for index, item := range contents {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := normalizePromptAuditRole(stringValue(content["role"]))
		if role == "" {
			role = "user"
		}
		parts, _ := content["parts"].([]any)
		for _, part := range parts {
			object, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text := stringValue(object["text"]); text != "" {
				result = append(result, promptAuditSegment{text: text, role: role, turn: turnOffset + index})
			}
		}
	}
	return result
}

func extractPromptAuditMedia(root map[string]any) []promptAuditSegment {
	if root == nil {
		return nil
	}
	texts := make([]string, 0, 4)
	seen := map[string]struct{}{}
	var walk func(any, string)
	walk = func(value any, key string) {
		switch typed := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for childKey := range typed {
				keys = append(keys, childKey)
			}
			sort.Strings(keys)
			for _, childKey := range keys {
				walk(typed[childKey], childKey)
			}
		case []any:
			for _, item := range typed {
				walk(item, key)
			}
		case string:
			text := strings.TrimSpace(typed)
			if !isPromptAuditMediaKey(key) || text == "" || looksLikePromptAuditMediaPayload(text) {
				return
			}
			if _, duplicate := seen[text]; duplicate {
				return
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
	}
	walk(root, "")
	result := make([]promptAuditSegment, 0, len(texts))
	for _, text := range texts {
		result = append(result, promptAuditSegment{text: text, role: "user", turn: 0})
	}
	return result
}

func selectPromptAuditTurns(values []promptAuditSegment) (current string, previous string) {
	segments := make([]promptAuditSegment, 0, len(values))
	for _, segment := range values {
		segment.text = strings.TrimSpace(segment.text)
		if segment.text != "" {
			segments = append(segments, segment)
		}
	}
	latestUser := -1
	for index := len(segments) - 1; index >= 0; index-- {
		if segments[index].role == "user" {
			latestUser = index
			break
		}
	}
	if latestUser < 0 {
		return "", ""
	}
	userTurn := segments[latestUser].turn
	userTexts := make([]string, 0, 2)
	firstUserIndex := latestUser
	for index, segment := range segments {
		if segment.role == "user" && segment.turn == userTurn {
			userTexts = append(userTexts, segment.text)
			if index < firstUserIndex {
				firstUserIndex = index
			}
		}
	}
	for index := firstUserIndex - 1; index >= 0; index-- {
		if segments[index].role != "assistant" && segments[index].role != "model" {
			continue
		}
		assistantTurn := segments[index].turn
		assistantTexts := make([]string, 0, 2)
		for _, segment := range segments {
			if (segment.role == "assistant" || segment.role == "model") && segment.turn == assistantTurn {
				assistantTexts = append(assistantTexts, segment.text)
			}
		}
		return strings.Join(userTexts, "\n\n"), strings.Join(assistantTexts, "\n\n")
	}
	return strings.Join(userTexts, "\n\n"), ""
}

func promptAuditContentTexts(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		result := make([]string, 0, len(typed))
		for _, part := range typed {
			object, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text := stringValue(object["text"]); text != "" {
				result = append(result, text)
			} else if transcript := stringValue(object["transcript"]); transcript != "" {
				result = append(result, transcript)
			}
		}
		return result
	case map[string]any:
		if text := stringValue(typed["text"]); text != "" {
			return []string{text}
		}
		if transcript := stringValue(typed["transcript"]); transcript != "" {
			return []string{transcript}
		}
	}
	return nil
}

func normalizePromptAuditRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant", "model":
		return strings.ToLower(strings.TrimSpace(role))
	case "system", "developer", "tool":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return ""
	}
}

func isPromptAuditMediaKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "prompt", "inputprompt", "textprompt", "description", "query", "lyrics", "negativeprompt",
		"positiveprompt", "gptdescriptionprompt", "prompten", "finalprompt", "finalzhprompt",
		"origprompt", "actualprompt", "imageprompt", "input", "content":
		return true
	default:
		return false
	}
}

func looksLikePromptAuditMediaPayload(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "data:image/") || strings.HasPrefix(lower, "data:video/") ||
		strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	if len(trimmed) < 256 {
		return false
	}
	for _, char := range trimmed {
		alphaNumeric := (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if !alphaNumeric && char != '+' && char != '/' && char != '=' {
			return false
		}
	}
	return true
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
