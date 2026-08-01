package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	ContentModerationProtocolAnthropicMessages = "anthropic_messages"
	ContentModerationProtocolOpenAIResponses   = "openai_responses"
	ContentModerationProtocolOpenAIChat        = "openai_chat_completions"
	ContentModerationProtocolGemini            = "gemini"
	ContentModerationProtocolOpenAIImages      = "openai_images"
	ContentModerationProtocolOpenAIRealtime    = "openai_realtime"
	ContentModerationProtocolAlphaSearch       = "openai_alpha_search"
	maxContentModerationInputRunes             = 12000
	maxContentModerationImages                 = 1
)

type ContentModerationInput struct {
	Text   string
	Images []string
}

func (input *ContentModerationInput) Normalize() {
	if input == nil {
		return
	}
	input.Text = contentModerationTrimRunes(normalizeContentModerationText(input.Text), maxContentModerationInputRunes)
	input.Images = normalizeContentModerationImages(input.Images)
}

func (input ContentModerationInput) Empty() bool {
	return strings.TrimSpace(input.Text) == "" && len(input.Images) == 0
}

func (input ContentModerationInput) Hash() string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("text:" + input.Text))
	for _, image := range input.Images {
		digest := sha256.Sum256([]byte(image))
		_, _ = hash.Write([]byte("\nimage:" + hex.EncodeToString(digest[:])))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (input ContentModerationInput) APIInput() any {
	images := limitContentModerationImages(input.Images)
	if len(images) == 0 {
		return input.Text
	}
	parts := make([]contentModerationAPIInputPart, 0, 2)
	if strings.TrimSpace(input.Text) != "" {
		parts = append(parts, contentModerationAPIInputPart{Type: "text", Text: input.Text})
	}
	parts = append(parts, contentModerationAPIInputPart{
		Type: "image_url", ImageURL: &contentModerationImageURL{URL: images[0]},
	})
	return parts
}

func ExtractContentModerationInput(protocol string, body []byte) ContentModerationInput {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ContentModerationInput{}
	}
	parts := make([]string, 0)
	images := make([]string, 0)
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		collectContentModerationLastAnthropicMessage(gjson.GetBytes(body, "messages"), &parts, &images)
	case ContentModerationProtocolOpenAIChat:
		collectContentModerationLastMessage(gjson.GetBytes(body, "messages"), "user", &parts, &images)
	case ContentModerationProtocolOpenAIResponses:
		collectContentModerationResponses(gjson.GetBytes(body, "input"), &parts, &images)
		if len(parts) == 0 && len(images) == 0 {
			collectContentModerationLastMessage(gjson.GetBytes(body, "messages"), "user", &parts, &images)
		}
	case ContentModerationProtocolOpenAIRealtime:
		collectContentModerationResponses(gjson.GetBytes(body, "input"), &parts, &images)
		collectContentModerationLastMessage(gjson.GetBytes(body, "messages"), "user", &parts, &images)
		item := gjson.GetBytes(body, "item")
		if role := strings.ToLower(strings.TrimSpace(item.Get("role").String())); role == "" || role == "user" {
			collectContentModerationValue(item.Get("content"), &parts, &images)
		}
		addContentModerationText(&parts, gjson.GetBytes(body, "response.instructions").String())
		addContentModerationText(&parts, gjson.GetBytes(body, "session.instructions").String())
	case ContentModerationProtocolGemini:
		collectContentModerationGemini(gjson.GetBytes(body, "contents"), &parts, &images)
	case ContentModerationProtocolOpenAIImages:
		addContentModerationText(&parts, gjson.GetBytes(body, "prompt").String())
		collectContentModerationValue(gjson.GetBytes(body, "images"), &parts, &images)
	case ContentModerationProtocolAlphaSearch:
		addContentModerationText(&parts, gjson.GetBytes(body, "query").String())
		addContentModerationText(&parts, gjson.GetBytes(body, "prompt").String())
	default:
		collectContentModerationResponses(gjson.GetBytes(body, "input"), &parts, &images)
		collectContentModerationLastMessage(gjson.GetBytes(body, "messages"), "user", &parts, &images)
		collectContentModerationGemini(gjson.GetBytes(body, "contents"), &parts, &images)
		addContentModerationText(&parts, gjson.GetBytes(body, "prompt").String())
		addContentModerationText(&parts, gjson.GetBytes(body, "query").String())
	}
	result := ContentModerationInput{Text: strings.Join(parts, "\n"), Images: images}
	result.Normalize()
	return result
}

func collectContentModerationLastMessage(messages gjson.Result, role string, parts, images *[]string) {
	if !messages.IsArray() {
		return
	}
	items := messages.Array()
	if len(items) == 0 {
		return
	}
	last := items[len(items)-1]
	if strings.ToLower(strings.TrimSpace(last.Get("role").String())) != role {
		return
	}
	var candidate []string
	var candidateImages []string
	collectContentModerationValue(last.Get("content"), &candidate, &candidateImages)
	if normalizeContentModerationText(strings.Join(candidate, "\n")) == "" && len(candidateImages) == 0 {
		return
	}
	*parts = append(*parts, candidate...)
	*images = append(*images, candidateImages...)
}

func collectContentModerationLastAnthropicMessage(messages gjson.Result, parts, images *[]string) {
	if !messages.IsArray() {
		return
	}
	items := messages.Array()
	if len(items) == 0 {
		return
	}
	last := items[len(items)-1]
	if strings.ToLower(strings.TrimSpace(last.Get("role").String())) != "user" {
		return
	}
	var candidate []string
	var candidateImages []string
	collectContentModerationAnthropicValue(last.Get("content"), &candidate, &candidateImages)
	if normalizeContentModerationText(strings.Join(candidate, "\n")) == "" && len(candidateImages) == 0 {
		return
	}
	*parts = append(*parts, candidate...)
	*images = append(*images, candidateImages...)
}

func collectContentModerationAnthropicValue(value gjson.Result, parts, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		if !strings.HasPrefix(strings.TrimSpace(value.String()), "<system-reminder>") {
			addContentModerationText(parts, value.String())
		}
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentModerationAnthropicValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "", "text", "input_text", "message":
			text := value.Get("text").String()
			if !strings.HasPrefix(strings.TrimSpace(text), "<system-reminder>") {
				addContentModerationText(parts, text)
			}
			if value.Get("content").Exists() {
				collectContentModerationAnthropicValue(value.Get("content"), parts, images)
			}
		case "image", "image_url", "input_image":
			collectContentModerationValue(value, parts, images)
		}
	}
}

func collectContentModerationResponses(input gjson.Result, parts, images *[]string) {
	switch {
	case !input.Exists():
		return
	case input.Type == gjson.String:
		addContentModerationText(parts, input.String())
	case input.IsArray():
		items := input.Array()
		if len(items) == 0 {
			return
		}
		last := items[len(items)-1]
		if contentModerationResponsesUserItem(last) {
			collectContentModerationValue(last.Get("content"), parts, images)
			if last.Get("type").String() == "input_text" || last.Get("text").Exists() {
				collectContentModerationValue(last, parts, images)
			}
		}
	case input.IsObject():
		if contentModerationResponsesUserItem(input) {
			collectContentModerationValue(input.Get("content"), parts, images)
			if input.Get("type").String() == "input_text" || input.Get("text").Exists() {
				collectContentModerationValue(input, parts, images)
			}
		}
	}
}

func contentModerationResponsesUserItem(item gjson.Result) bool {
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	if role != "" && role != "user" {
		return false
	}
	var parts []string
	var images []string
	collectContentModerationValue(item.Get("content"), &parts, &images)
	if item.Get("type").String() == "input_text" || item.Get("text").Exists() {
		collectContentModerationValue(item, &parts, &images)
	}
	return normalizeContentModerationText(strings.Join(parts, "\n")) != "" || len(images) > 0
}

func collectContentModerationGemini(contents gjson.Result, parts, images *[]string) {
	if !contents.IsArray() {
		return
	}
	items := contents.Array()
	if len(items) == 0 {
		return
	}
	last := items[len(items)-1]
	role := strings.ToLower(strings.TrimSpace(last.Get("role").String()))
	if role != "" && role != "user" {
		return
	}
	var candidate []string
	var candidateImages []string
	if values := last.Get("parts"); values.IsArray() {
		values.ForEach(func(_, part gjson.Result) bool {
			addContentModerationText(&candidate, part.Get("text").String())
			addContentModerationImageData(&candidateImages, part.Get("inline_data.mime_type").String(), part.Get("inline_data.data").String())
			addContentModerationImageData(&candidateImages, part.Get("inlineData.mimeType").String(), part.Get("inlineData.data").String())
			addContentModerationImage(&candidateImages, part.Get("file_data.file_uri").String())
			addContentModerationImage(&candidateImages, part.Get("fileData.fileUri").String())
			return true
		})
	}
	if normalizeContentModerationText(strings.Join(candidate, "\n")) == "" && len(candidateImages) == 0 {
		return
	}
	*parts = append(*parts, candidate...)
	*images = append(*images, candidateImages...)
}

func collectContentModerationValue(value gjson.Result, parts, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addContentModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentModerationValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		addContentModerationImage(images, value.Get("image_url.url").String())
		addContentModerationImage(images, value.Get("image_url").String())
		addContentModerationImage(images, value.Get("url").String())
		addContentModerationImageData(images, value.Get("source.media_type").String(), value.Get("source.data").String())
		addContentModerationImageData(images, value.Get("source.mediaType").String(), value.Get("source.data").String())
		addContentModerationImageData(images, value.Get("inline_data.mime_type").String(), value.Get("inline_data.data").String())
		addContentModerationImageData(images, value.Get("inlineData.mimeType").String(), value.Get("inlineData.data").String())
		addContentModerationImage(images, value.Get("file_data.file_uri").String())
		addContentModerationImage(images, value.Get("fileData.fileUri").String())
		if typ == "" || typ == "text" || typ == "input_text" || typ == "message" {
			addContentModerationText(parts, value.Get("text").String())
			if value.Get("content").Exists() {
				collectContentModerationValue(value.Get("content"), parts, images)
			}
		}
	}
}

func addContentModerationText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	*parts = append(*parts, text)
}

func addContentModerationImageData(images *[]string, mediaType, data string) {
	mediaType, data = strings.TrimSpace(mediaType), strings.TrimSpace(data)
	if mediaType != "" && data != "" {
		addContentModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mediaType, data))
	}
}

func addContentModerationImage(images *[]string, image string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	if strings.HasPrefix(image, "data:image/") || strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		*images = append(*images, image)
	}
}

func normalizeContentModerationImages(images []string) []string {
	result := make([]string, 0, len(images))
	seen := map[string]struct{}{}
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		result = append(result, image)
	}
	return result
}

func limitContentModerationImages(images []string) []string {
	if len(images) <= maxContentModerationImages {
		return images
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(images))))
	if err != nil {
		return images[:maxContentModerationImages]
	}
	return []string{images[index.Int64()]}
}

func normalizeContentModerationText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func contentModerationTrimRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
