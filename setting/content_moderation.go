package setting

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	ContentModerationConfigOptionKey = "ContentModerationConfig"

	ContentModerationModeOff      = "off"
	ContentModerationModeObserve  = "observe"
	ContentModerationModePreBlock = "pre_block"

	ContentModerationKeywordOnly   = "keyword_only"
	ContentModerationKeywordAndAPI = "keyword_and_api"
	ContentModerationAPIOnly       = "api_only"

	ContentModerationModelAll     = "all"
	ContentModerationModelInclude = "include"
	ContentModerationModelExclude = "exclude"

	DefaultContentModerationBaseURL   = "https://api.openai.com"
	DefaultContentModerationModel     = "omni-moderation-latest"
	DefaultContentModerationTimeoutMS = 3000
	DefaultContentModerationWorkers   = 4
	DefaultContentModerationQueueSize = 32768
	DefaultCyberSessionBlockTTL       = 3600
	MaxContentModerationWorkers       = 32
	MaxContentModerationQueueSize     = 100000
)

var ContentModerationConfigJSON string

var ContentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

type ContentModerationModelFilter struct {
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

type ContentModerationStorageConfig struct {
	Enabled                        bool                         `json:"enabled"`
	Mode                           string                       `json:"mode"`
	BaseURL                        string                       `json:"base_url"`
	Model                          string                       `json:"model"`
	ProxyURL                       string                       `json:"proxy_url"`
	APIKeyCiphertexts              []string                     `json:"api_key_ciphertexts,omitempty"`
	TimeoutMS                      int                          `json:"timeout_ms"`
	SampleRate                     int                          `json:"sample_rate"`
	AllGroups                      bool                         `json:"all_groups"`
	Groups                         []string                     `json:"groups"`
	RecordNonHits                  bool                         `json:"record_non_hits"`
	Thresholds                     map[string]float64           `json:"thresholds"`
	WorkerCount                    int                          `json:"worker_count"`
	QueueSize                      int                          `json:"queue_size"`
	BlockStatus                    int                          `json:"block_status"`
	BlockMessage                   string                       `json:"block_message"`
	EmailOnHit                     bool                         `json:"email_on_hit"`
	AutoBanEnabled                 bool                         `json:"auto_ban_enabled"`
	BanThreshold                   int                          `json:"ban_threshold"`
	ViolationWindowHours           int                          `json:"violation_window_hours"`
	RetryCount                     int                          `json:"retry_count"`
	HitRetentionDays               int                          `json:"hit_retention_days"`
	NonHitRetentionDays            int                          `json:"non_hit_retention_days"`
	PreHashCheckEnabled            bool                         `json:"pre_hash_check_enabled"`
	BlockedKeywords                []string                     `json:"blocked_keywords"`
	KeywordBlockingMode            string                       `json:"keyword_blocking_mode"`
	ModelFilter                    ContentModerationModelFilter `json:"model_filter"`
	CyberPolicyExcludeFromBanCount bool                         `json:"cyber_policy_exclude_from_ban_count"`
	CyberSessionBlockEnabled       bool                         `json:"cyber_session_block_enabled"`
	CyberSessionBlockTTLSeconds    int                          `json:"cyber_session_block_ttl_seconds"`
	ConfigVersion                  int64                        `json:"config_version"`
	UpdatedAt                      time.Time                    `json:"updated_at"`
	UpdatedBy                      int                          `json:"updated_by"`
}

func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment": 0.98, "harassment/threatening": 0.90,
		"hate": 0.65, "hate/threatening": 0.65,
		"illicit": 0.95, "illicit/violent": 0.95,
		"self-harm": 0.65, "self-harm/intent": 0.85,
		"self-harm/instructions": 0.65, "sexual": 0.65,
		"sexual/minors": 0.65, "violence": 0.95,
		"violence/graphic": 0.95,
	}
}

func DefaultContentModerationStorageConfig() ContentModerationStorageConfig {
	return ContentModerationStorageConfig{
		Enabled: false, Mode: ContentModerationModePreBlock,
		BaseURL: DefaultContentModerationBaseURL, Model: DefaultContentModerationModel,
		TimeoutMS: DefaultContentModerationTimeoutMS, SampleRate: 100,
		AllGroups: true, Groups: []string{}, RecordNonHits: false,
		Thresholds:  ContentModerationDefaultThresholds(),
		WorkerCount: DefaultContentModerationWorkers, QueueSize: DefaultContentModerationQueueSize,
		BlockStatus: 403, BlockMessage: "内容审核命中风险规则，请调整输入后重试",
		EmailOnHit: true, AutoBanEnabled: true, BanThreshold: 10,
		ViolationWindowHours: 720, RetryCount: 2,
		HitRetentionDays: 180, NonHitRetentionDays: 3,
		PreHashCheckEnabled: false, BlockedKeywords: []string{},
		KeywordBlockingMode:            ContentModerationKeywordAndAPI,
		ModelFilter:                    ContentModerationModelFilter{Type: ContentModerationModelAll, Models: []string{}},
		CyberPolicyExcludeFromBanCount: false, CyberSessionBlockEnabled: false,
		CyberSessionBlockTTLSeconds: DefaultCyberSessionBlockTTL,
		ConfigVersion:               1,
	}
}

func GetContentModerationStorageConfig() (ContentModerationStorageConfig, error) {
	config := DefaultContentModerationStorageConfig()
	raw := strings.TrimSpace(ContentModerationConfigJSON)
	if raw == "" {
		return config, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return ContentModerationStorageConfig{}, fmt.Errorf("invalid content moderation config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ContentModerationStorageConfig{}, fmt.Errorf("invalid content moderation config: trailing JSON data")
	}
	NormalizeContentModerationStorageConfig(&config)
	if err := ValidateContentModerationStorageConfig(config); err != nil {
		return ContentModerationStorageConfig{}, err
	}
	return config, nil
}

func NormalizeContentModerationStorageConfig(config *ContentModerationStorageConfig) {
	if config == nil {
		return
	}
	if config.ConfigVersion < 1 {
		config.ConfigVersion = 1
	}
	if config.Mode == "" {
		config.Mode = ContentModerationModePreBlock
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if config.BaseURL == "" {
		config.BaseURL = DefaultContentModerationBaseURL
	}
	config.Model = strings.TrimSpace(config.Model)
	if config.Model == "" {
		config.Model = DefaultContentModerationModel
	}
	config.ProxyURL = strings.TrimSpace(config.ProxyURL)
	if config.TimeoutMS <= 0 {
		config.TimeoutMS = DefaultContentModerationTimeoutMS
	}
	if config.SampleRate < 0 {
		config.SampleRate = 0
	}
	if config.SampleRate > 100 {
		config.SampleRate = 100
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = DefaultContentModerationWorkers
	}
	if config.QueueSize <= 0 {
		config.QueueSize = DefaultContentModerationQueueSize
	}
	if config.BlockStatus == 0 {
		config.BlockStatus = 403
	}
	config.BlockMessage = strings.TrimSpace(config.BlockMessage)
	if config.BlockMessage == "" {
		config.BlockMessage = "内容审核命中风险规则，请调整输入后重试"
	}
	if config.BanThreshold <= 0 {
		config.BanThreshold = 10
	}
	if config.ViolationWindowHours <= 0 {
		config.ViolationWindowHours = 720
	}
	if config.RetryCount < 0 {
		config.RetryCount = 0
	}
	if config.RetryCount > 5 {
		config.RetryCount = 5
	}
	if config.HitRetentionDays <= 0 {
		config.HitRetentionDays = 180
	}
	if config.NonHitRetentionDays <= 0 {
		config.NonHitRetentionDays = 3
	}
	if config.CyberSessionBlockTTLSeconds <= 0 {
		config.CyberSessionBlockTTLSeconds = DefaultCyberSessionBlockTTL
	}
	config.Groups = normalizeContentModerationStrings(config.Groups, 256, 128)
	config.BlockedKeywords = normalizeContentModerationStrings(config.BlockedKeywords, 10000, 200)
	config.APIKeyCiphertexts = normalizeContentModerationStrings(config.APIKeyCiphertexts, 256, 32768)
	config.Thresholds = mergeContentModerationThresholds(config.Thresholds)
	config.KeywordBlockingMode = normalizeContentModerationKeywordMode(config.KeywordBlockingMode)
	config.ModelFilter.Type = normalizeContentModerationModelFilterType(config.ModelFilter.Type)
	config.ModelFilter.Models = normalizeContentModerationStrings(config.ModelFilter.Models, 1000, 200)
	if config.ModelFilter.Type == ContentModerationModelAll {
		config.ModelFilter.Models = []string{}
	}
}

func ValidateContentModerationStorageConfig(config ContentModerationStorageConfig) error {
	NormalizeContentModerationStorageConfig(&config)
	switch config.Mode {
	case ContentModerationModeOff, ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return fmt.Errorf("unsupported content moderation mode: %s", config.Mode)
	}
	parsed, err := url.ParseRequestURI(config.BaseURL)
	if err != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("content moderation base URL must be an absolute HTTP URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("content moderation base URL must not contain credentials, query parameters, or fragments")
	}
	if _, err := common.ParseProxyURLStrict(config.ProxyURL); err != nil {
		return fmt.Errorf("invalid content moderation proxy URL: %w", err)
	}
	if len(config.Model) > 256 {
		return fmt.Errorf("content moderation model must not exceed 256 bytes")
	}
	if config.TimeoutMS < 100 || config.TimeoutMS > 30000 {
		return fmt.Errorf("content moderation timeout must be between 100 and 30000 ms")
	}
	if config.WorkerCount < 1 || config.WorkerCount > MaxContentModerationWorkers {
		return fmt.Errorf("content moderation worker count must be between 1 and %d", MaxContentModerationWorkers)
	}
	if config.QueueSize < 1 || config.QueueSize > MaxContentModerationQueueSize {
		return fmt.Errorf("content moderation queue size must be between 1 and %d", MaxContentModerationQueueSize)
	}
	if config.BlockStatus < 400 || config.BlockStatus > 599 {
		return fmt.Errorf("content moderation block status must be between 400 and 599")
	}
	if config.HitRetentionDays > 3650 || config.NonHitRetentionDays > 3 {
		return fmt.Errorf("content moderation retention exceeds the supported range")
	}
	if config.CyberSessionBlockTTLSeconds < 60 || config.CyberSessionBlockTTLSeconds > 604800 {
		return fmt.Errorf("cyber session block TTL must be between 60 and 604800 seconds")
	}
	if config.ModelFilter.Type != ContentModerationModelAll && len(config.ModelFilter.Models) == 0 {
		return fmt.Errorf("content moderation model filter requires at least one model")
	}
	return nil
}

func ContentModerationStorageActive(config ContentModerationStorageConfig) bool {
	return config.Enabled && config.Mode != ContentModerationModeOff
}

func ContentModerationCyberPolicyActive(config ContentModerationStorageConfig) bool {
	return config.Enabled
}

func ContentModerationJSONActive(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	config := DefaultContentModerationStorageConfig()
	if json.Unmarshal([]byte(raw), &config) != nil {
		return false
	}
	NormalizeContentModerationStorageConfig(&config)
	return ContentModerationStorageActive(config)
}

func DecryptContentModerationAPIKeys(config ContentModerationStorageConfig) ([]string, []string) {
	keys := make([]string, 0, len(config.APIKeyCiphertexts))
	invalid := make([]string, 0)
	for _, ciphertext := range config.APIKeyCiphertexts {
		plaintext, err := DecryptPromptAuditToken(ciphertext)
		if err != nil || strings.TrimSpace(plaintext) == "" {
			invalid = append(invalid, ciphertext)
			continue
		}
		keys = append(keys, strings.TrimSpace(plaintext))
	}
	return keys, invalid
}

func EncryptContentModerationAPIKey(value string) (string, error) {
	return EncryptPromptAuditToken(value)
}

func normalizeContentModerationKeywordMode(value string) string {
	switch strings.TrimSpace(value) {
	case ContentModerationKeywordOnly, ContentModerationAPIOnly, ContentModerationKeywordAndAPI:
		return strings.TrimSpace(value)
	default:
		return ContentModerationKeywordAndAPI
	}
}

func normalizeContentModerationModelFilterType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ContentModerationModelInclude, ContentModerationModelExclude:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ContentModerationModelAll
	}
}

func normalizeContentModerationStrings(values []string, maxItems, maxRunes int) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		runes := []rune(value)
		if len(runes) > maxRunes {
			value = string(runes[:maxRunes])
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) >= maxItems {
			break
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func mergeContentModerationThresholds(values map[string]float64) map[string]float64 {
	result := ContentModerationDefaultThresholds()
	for _, category := range ContentModerationCategoryOrder {
		if value, exists := values[category]; exists {
			if value < 0 {
				value = 0
			}
			if value > 1 {
				value = 1
			}
			result[category] = value
		}
	}
	return result
}
