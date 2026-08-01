package setting

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPromptAuditBaseURL        = "http://qwen3guard:11434"
	DefaultPromptAuditModel          = "sileader/qwen3guard:0.6b"
	DefaultPromptAuditTimeoutMS      = 10000
	DefaultPromptAuditInputLimit     = 8000
	DefaultPromptAuditMaxConcurrency = 4
	maxPromptAuditTimeoutMS          = 120000
	maxPromptAuditInputLimit         = 100000
	maxPromptAuditMaxConcurrency     = 128
	maxPromptAuditGroupPolicies      = 256
	maxPromptAuditGroupPoliciesBytes = 256 << 10
	maxPromptAuditGroupNameBytes     = 128
	DefaultPromptAuditWorkerCount    = 4
	DefaultPromptAuditQueueCapacity  = 32768
	MaxPromptAuditWorkerCount        = 32
	MaxPromptAuditQueueCapacity      = 100000
	MaxPromptAuditEndpoints          = 32
	PromptAuditConfigOptionKey       = "PromptAuditConfig"
	PromptAuditEncryptionKeyEnv      = "PROMPT_AUDIT_ENCRYPTION_KEY"
	PromptAuditFallbackKeyEnv        = "TOTP_ENCRYPTION_KEY"
)

type PromptAuditMode string

const (
	PromptAuditModeOff      PromptAuditMode = "off"
	PromptAuditModeAsync    PromptAuditMode = "async_audit"
	PromptAuditModeBlocking PromptAuditMode = "blocking"
)

var PromptAuditScannerIDs = []string{
	"violent",
	"non_violent_illegal_acts",
	"sexual_content_or_sexual_acts",
	"pii",
	"suicide_and_self_harm",
	"unethical_acts",
	"politically_sensitive_topics",
	"copyright_violation",
	"jailbreak",
}

var PromptAuditEnabled = false
var PromptAuditBaseURL = DefaultPromptAuditBaseURL
var PromptAuditModel = DefaultPromptAuditModel
var PromptAuditAPIKey = ""
var PromptAuditTimeoutMS = DefaultPromptAuditTimeoutMS
var PromptAuditFailClosed = true
var PromptAuditInputLimit = DefaultPromptAuditInputLimit
var PromptAuditMaxConcurrency = DefaultPromptAuditMaxConcurrency
var PromptAuditScanners = strings.Join(PromptAuditScannerIDs, ",")
var PromptAuditGroupPolicies = "{}"
var PromptAuditConfigJSON = ""

type PromptAuditEndpoint struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	TokenCiphertext string `json:"token_ciphertext,omitempty"`
	TimeoutMS       int    `json:"timeout_ms"`
	InputLimit      int    `json:"input_limit"`
	Enabled         bool   `json:"enabled"`
}

type PromptAuditGroupPolicy struct {
	Mode       PromptAuditMode `json:"mode,omitempty"`
	Enabled    bool            `json:"enabled"`
	FailClosed bool            `json:"fail_closed"`
	Scanners   []string        `json:"scanners"`
}

type PromptAuditStorageConfig struct {
	Mode                   PromptAuditMode                   `json:"mode"`
	BlockingLatestTurnOnly bool                              `json:"blocking_latest_turn_only"`
	StorePassEvents        bool                              `json:"store_pass_events"`
	StoreBlockedEventsOnly bool                              `json:"store_blocked_events_only"`
	Strategy               string                            `json:"strategy"`
	WorkerCount            int                               `json:"worker_count"`
	QueueCapacity          int                               `json:"queue_capacity"`
	Scanners               []string                          `json:"scanners"`
	FailClosed             bool                              `json:"fail_closed"`
	Endpoints              []PromptAuditEndpoint             `json:"endpoints"`
	GroupPolicies          map[string]PromptAuditGroupPolicy `json:"group_policies"`
	ConfigVersion          int64                             `json:"config_version"`
	UpdatedAt              time.Time                         `json:"updated_at"`
	UpdatedBy              int                               `json:"updated_by"`
}

type PromptAuditConfig struct {
	Enabled                bool
	Mode                   PromptAuditMode
	BaseURL                string
	Model                  string
	APIKey                 string
	TimeoutMS              int
	FailClosed             bool
	InputLimit             int
	MaxConcurrency         int
	Scanners               []string
	BlockingLatestTurnOnly bool
	StorePassEvents        bool
	StoreBlockedEventsOnly bool
	WorkerCount            int
	QueueCapacity          int
	Strategy               string
	ConfigVersion          int64
	Group                  string
	Endpoints              []PromptAuditEndpoint
}

func DefaultPromptAuditStorageConfig() PromptAuditStorageConfig {
	scanners, _ := ParsePromptAuditScanners(PromptAuditScanners)
	mode := PromptAuditModeOff
	if PromptAuditEnabled {
		mode = PromptAuditModeBlocking
	}
	return PromptAuditStorageConfig{
		Mode:                   mode,
		BlockingLatestTurnOnly: true,
		StorePassEvents:        false,
		StoreBlockedEventsOnly: false,
		Strategy:               "priority",
		WorkerCount:            DefaultPromptAuditWorkerCount,
		QueueCapacity:          DefaultPromptAuditQueueCapacity,
		Scanners:               scanners,
		FailClosed:             PromptAuditFailClosed,
		Endpoints: []PromptAuditEndpoint{{
			ID: "qwen3guard-primary", Name: "Qwen3Guard", Protocol: "openai_compatible",
			BaseURL: strings.TrimSpace(PromptAuditBaseURL), Model: strings.TrimSpace(PromptAuditModel),
			TimeoutMS: PromptAuditTimeoutMS, InputLimit: PromptAuditInputLimit, Enabled: true,
		}},
		GroupPolicies: map[string]PromptAuditGroupPolicy{},
		ConfigVersion: 1,
	}
}

func GetPromptAuditStorageConfig() (PromptAuditStorageConfig, error) {
	raw := strings.TrimSpace(PromptAuditConfigJSON)
	if raw == "" {
		config := DefaultPromptAuditStorageConfig()
		if policies, err := ParsePromptAuditGroupPolicies(PromptAuditGroupPolicies); err == nil {
			config.GroupPolicies = policies
		}
		normalizePromptAuditStorageConfig(&config)
		return config, nil
	}
	config := DefaultPromptAuditStorageConfig()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return PromptAuditStorageConfig{}, fmt.Errorf("invalid prompt audit config: %w", err)
	}
	if err := ensurePromptAuditJSONEOF(decoder); err != nil {
		return PromptAuditStorageConfig{}, err
	}
	normalizePromptAuditStorageConfig(&config)
	if err := ValidatePromptAuditStorageConfig(config); err != nil {
		return PromptAuditStorageConfig{}, err
	}
	return config, nil
}

func PromptAuditStorageActive(config PromptAuditStorageConfig) bool {
	if config.Mode != PromptAuditModeOff {
		return true
	}
	for _, policy := range config.GroupPolicies {
		if promptAuditEffectivePolicyMode(config.Mode, policy) != PromptAuditModeOff {
			return true
		}
	}
	return false
}

func PromptAuditJSONActive(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		config, err := GetPromptAuditStorageConfig()
		return err == nil && PromptAuditStorageActive(config)
	}
	config := DefaultPromptAuditStorageConfig()
	if json.Unmarshal([]byte(raw), &config) != nil {
		return false
	}
	normalizePromptAuditStorageConfig(&config)
	return PromptAuditStorageActive(config)
}

func GetPromptAuditConfig() PromptAuditConfig {
	config, err := GetPromptAuditStorageConfig()
	if err != nil {
		return PromptAuditConfig{Enabled: PromptAuditEnabled, Mode: PromptAuditModeBlocking, FailClosed: true}
	}
	return promptAuditActiveConfig(config, "")
}

func GetPromptAuditConfigForGroup(group string) PromptAuditConfig {
	storage, err := GetPromptAuditStorageConfig()
	if err != nil {
		return PromptAuditConfig{Enabled: true, Mode: PromptAuditModeBlocking, FailClosed: true, Group: strings.TrimSpace(group)}
	}
	return promptAuditActiveConfig(storage, strings.TrimSpace(group))
}

func promptAuditActiveConfig(storage PromptAuditStorageConfig, group string) PromptAuditConfig {
	mode := storage.Mode
	scanners := append([]string(nil), storage.Scanners...)
	if policy, ok := storage.GroupPolicies[group]; ok {
		mode = promptAuditEffectivePolicyMode(storage.Mode, policy)
		scanners = append([]string(nil), policy.Scanners...)
	}
	config := PromptAuditConfig{
		Enabled: mode != PromptAuditModeOff, Mode: mode, FailClosed: true,
		MaxConcurrency: PromptAuditMaxConcurrency, Scanners: scanners,
		BlockingLatestTurnOnly: storage.BlockingLatestTurnOnly, StorePassEvents: storage.StorePassEvents,
		StoreBlockedEventsOnly: storage.StoreBlockedEventsOnly,
		WorkerCount:            storage.WorkerCount, QueueCapacity: storage.QueueCapacity, Strategy: storage.Strategy,
		ConfigVersion: storage.ConfigVersion, Group: group,
	}
	for _, endpoint := range storage.Endpoints {
		if endpoint.Enabled {
			config.Endpoints = append(config.Endpoints, endpoint)
		}
	}
	if len(config.Endpoints) > 0 {
		first := config.Endpoints[0]
		config.BaseURL, config.Model = first.BaseURL, first.Model
		config.TimeoutMS, config.InputLimit = first.TimeoutMS, first.InputLimit
		if first.TokenCiphertext != "" {
			config.APIKey, _ = DecryptPromptAuditToken(first.TokenCiphertext)
		} else if strings.TrimSpace(PromptAuditConfigJSON) == "" {
			config.APIKey = strings.TrimSpace(PromptAuditAPIKey)
		}
	}
	return config
}

func promptAuditEffectivePolicyMode(defaultMode PromptAuditMode, policy PromptAuditGroupPolicy) PromptAuditMode {
	if policy.Mode != "" {
		return policy.Mode
	}
	if !policy.Enabled {
		return PromptAuditModeOff
	}
	if defaultMode == PromptAuditModeOff {
		// An enabled-only legacy policy opts its group into blocking audit.
		return PromptAuditModeBlocking
	}
	return defaultMode
}

func normalizePromptAuditStorageConfig(config *PromptAuditStorageConfig) {
	config.FailClosed = true
	if config.ConfigVersion < 1 {
		config.ConfigVersion = 1
	}
	if config.Strategy == "" {
		config.Strategy = "priority"
	}
	if config.WorkerCount == 0 {
		config.WorkerCount = DefaultPromptAuditWorkerCount
	}
	if config.QueueCapacity == 0 {
		config.QueueCapacity = DefaultPromptAuditQueueCapacity
	}
	if len(config.Scanners) == 0 {
		config.Scanners = append([]string(nil), PromptAuditScannerIDs...)
	}
	if config.GroupPolicies == nil {
		config.GroupPolicies = map[string]PromptAuditGroupPolicy{}
	}
	for index := range config.Endpoints {
		endpoint := &config.Endpoints[index]
		endpoint.ID = strings.TrimSpace(endpoint.ID)
		endpoint.Name = strings.TrimSpace(endpoint.Name)
		endpoint.Protocol = strings.TrimSpace(endpoint.Protocol)
		if endpoint.Protocol == "" {
			endpoint.Protocol = "openai_compatible"
		}
		endpoint.BaseURL = strings.TrimSpace(endpoint.BaseURL)
		endpoint.Model = strings.TrimSpace(endpoint.Model)
		if endpoint.Model == "" {
			endpoint.Model = DefaultPromptAuditModel
		}
		if endpoint.TimeoutMS == 0 {
			endpoint.TimeoutMS = DefaultPromptAuditTimeoutMS
		}
		if endpoint.InputLimit == 0 {
			endpoint.InputLimit = DefaultPromptAuditInputLimit
		}
	}
	for group, policy := range config.GroupPolicies {
		policy.Mode = promptAuditEffectivePolicyMode(config.Mode, policy)
		policy.Enabled = policy.Mode != PromptAuditModeOff
		policy.FailClosed = true
		config.GroupPolicies[group] = policy
	}
}

func ValidatePromptAuditStorageConfig(config PromptAuditStorageConfig) error {
	if config.Mode != PromptAuditModeOff && config.Mode != PromptAuditModeAsync && config.Mode != PromptAuditModeBlocking {
		return fmt.Errorf("unsupported prompt audit mode: %s", config.Mode)
	}
	auditEnabled := config.Mode != PromptAuditModeOff
	if config.Strategy != "priority" {
		return fmt.Errorf("prompt audit strategy must be priority")
	}
	if config.WorkerCount < 1 || config.WorkerCount > MaxPromptAuditWorkerCount {
		return fmt.Errorf("prompt audit worker count must be between 1 and %d", MaxPromptAuditWorkerCount)
	}
	if config.QueueCapacity < 1 || config.QueueCapacity > MaxPromptAuditQueueCapacity {
		return fmt.Errorf("prompt audit queue capacity must be between 1 and %d", MaxPromptAuditQueueCapacity)
	}
	if _, err := ParsePromptAuditScanners(strings.Join(config.Scanners, ",")); err != nil {
		return err
	}
	if len(config.Endpoints) > MaxPromptAuditEndpoints {
		return fmt.Errorf("prompt audit config must not contain more than %d endpoints", MaxPromptAuditEndpoints)
	}
	seen := map[string]struct{}{}
	enabled := 0
	for _, endpoint := range config.Endpoints {
		if endpoint.ID == "" || endpoint.Name == "" {
			return fmt.Errorf("prompt audit endpoint ID and name are required")
		}
		if _, duplicate := seen[endpoint.ID]; duplicate {
			return fmt.Errorf("duplicate prompt audit endpoint ID: %s", endpoint.ID)
		}
		seen[endpoint.ID] = struct{}{}
		if endpoint.Protocol != "openai_compatible" {
			return fmt.Errorf("prompt audit endpoint protocol must be openai_compatible")
		}
		if err := ValidatePromptAuditBaseURL(endpoint.BaseURL); err != nil {
			return err
		}
		if err := ValidatePromptAuditModel(endpoint.Model); err != nil {
			return err
		}
		if endpoint.TimeoutMS < 100 || endpoint.TimeoutMS > 120000 {
			return fmt.Errorf("prompt audit endpoint timeout must be between 100 and 120000")
		}
		if endpoint.InputLimit < 128 || endpoint.InputLimit > 100000 {
			return fmt.Errorf("prompt audit endpoint input limit must be between 128 and 100000")
		}
		if endpoint.Enabled {
			enabled++
		}
	}
	for group, policy := range config.GroupPolicies {
		if group == "" || strings.TrimSpace(group) != group || len(group) > maxPromptAuditGroupNameBytes {
			return fmt.Errorf("invalid prompt audit group name")
		}
		mode := promptAuditEffectivePolicyMode(config.Mode, policy)
		if mode != PromptAuditModeOff && mode != PromptAuditModeAsync && mode != PromptAuditModeBlocking {
			return fmt.Errorf("invalid prompt audit mode for group %s", group)
		}
		if mode != PromptAuditModeOff {
			auditEnabled = true
		}
		if _, err := ParsePromptAuditScanners(strings.Join(policy.Scanners, ",")); err != nil {
			return fmt.Errorf("invalid prompt audit policy for group %s: %w", group, err)
		}
	}
	if auditEnabled && enabled == 0 {
		return fmt.Errorf("at least one prompt audit endpoint must be enabled")
	}
	return nil
}

func PromptAuditEncryptionKeyConfigured() bool {
	_, err := promptAuditEncryptionKey()
	return err == nil
}

func EncryptPromptAuditToken(value string) (string, error) {
	key, err := promptAuditEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(strings.TrimSpace(value)), nil)
	return "v1:" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func DecryptPromptAuditToken(value string) (string, error) {
	key, err := promptAuditEncryptionKey()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(value, "v1:") {
		return "", fmt.Errorf("unsupported prompt audit token ciphertext")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("prompt audit token ciphertext is truncated")
	}
	plaintext, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func promptAuditEncryptionKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(PromptAuditEncryptionKeyEnv))
	keyName := PromptAuditEncryptionKeyEnv
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(PromptAuditFallbackKeyEnv))
		keyName = PromptAuditFallbackKeyEnv
	}
	if raw == "" {
		return nil, fmt.Errorf("%s or %s must be configured before storing prompt audit tokens", PromptAuditEncryptionKeyEnv, PromptAuditFallbackKeyEnv)
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s must be 64 hexadecimal characters", keyName)
	}
	return key, nil
}

func ValidatePromptAuditBaseURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("prompt audit base URL is required")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed == nil || parsed.Host == "" {
		return fmt.Errorf("prompt audit base URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("prompt audit base URL must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("prompt audit base URL must not contain credentials, query parameters, or fragments")
	}
	return nil
}

func ValidatePromptAuditModel(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("prompt audit model is required")
	}
	if len(value) > 256 {
		return fmt.Errorf("prompt audit model must not exceed 256 bytes")
	}
	return nil
}

func ParsePromptAuditTimeoutMS(value string) (int, error) {
	return parsePromptAuditInt(value, 1, maxPromptAuditTimeoutMS, "timeout")
}

func ParsePromptAuditInputLimit(value string) (int, error) {
	return parsePromptAuditInt(value, 1, maxPromptAuditInputLimit, "input limit")
}

func ParsePromptAuditMaxConcurrency(value string) (int, error) {
	return parsePromptAuditInt(value, 1, maxPromptAuditMaxConcurrency, "max concurrency")
}

func parsePromptAuditInt(value string, minimum int, maximum int, name string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("prompt audit %s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func ParsePromptAuditScanners(value string) ([]string, error) {
	allowed := make(map[string]struct{}, len(PromptAuditScannerIDs))
	for _, scanner := range PromptAuditScannerIDs {
		allowed[scanner] = struct{}{}
	}
	seen := make(map[string]struct{}, len(PromptAuditScannerIDs))
	result := make([]string, 0, len(PromptAuditScannerIDs))
	for _, raw := range strings.Split(value, ",") {
		scanner := strings.ToLower(strings.TrimSpace(raw))
		if scanner == "" {
			continue
		}
		if _, ok := allowed[scanner]; !ok {
			return nil, fmt.Errorf("unsupported prompt audit scanner: %s", scanner)
		}
		if _, duplicate := seen[scanner]; duplicate {
			continue
		}
		seen[scanner] = struct{}{}
		result = append(result, scanner)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one prompt audit scanner is required")
	}
	return result, nil
}

func ParsePromptAuditGroupPolicies(value string) (map[string]PromptAuditGroupPolicy, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "{}"
	}
	if len(value) > maxPromptAuditGroupPoliciesBytes {
		return nil, fmt.Errorf("prompt audit group policies must not exceed %d bytes", maxPromptAuditGroupPoliciesBytes)
	}
	policies := map[string]PromptAuditGroupPolicy{}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policies); err != nil {
		return nil, fmt.Errorf("invalid prompt audit group policies: %w", err)
	}
	if policies == nil {
		return nil, fmt.Errorf("prompt audit group policies must be a JSON object")
	}
	if err := ensurePromptAuditJSONEOF(decoder); err != nil {
		return nil, err
	}
	if len(policies) > maxPromptAuditGroupPolicies {
		return nil, fmt.Errorf("prompt audit group policies must not contain more than %d groups", maxPromptAuditGroupPolicies)
	}
	for group, policy := range policies {
		if group == "" || strings.TrimSpace(group) != group {
			return nil, fmt.Errorf("prompt audit group names must be non-empty and must not have surrounding whitespace")
		}
		if len(group) > maxPromptAuditGroupNameBytes {
			return nil, fmt.Errorf("prompt audit group name must not exceed %d bytes", maxPromptAuditGroupNameBytes)
		}
		scanners, err := ParsePromptAuditScanners(strings.Join(policy.Scanners, ","))
		if err != nil {
			return nil, fmt.Errorf("invalid prompt audit policy for group %s: %w", group, err)
		}
		policy.Scanners = scanners
		policies[group] = policy
	}
	return policies, nil
}

func ensurePromptAuditJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid prompt audit group policies: trailing JSON value")
		}
		return fmt.Errorf("invalid prompt audit group policies: %w", err)
	}
	return nil
}

func ValidatePromptAuditConfig(config PromptAuditConfig) error {
	if config.Mode != "" && config.Mode != PromptAuditModeOff && config.Mode != PromptAuditModeAsync && config.Mode != PromptAuditModeBlocking {
		return fmt.Errorf("unsupported prompt audit mode: %s", config.Mode)
	}
	if len(config.Endpoints) > 0 {
		for _, endpoint := range config.Endpoints {
			if !endpoint.Enabled {
				continue
			}
			if err := ValidatePromptAuditBaseURL(endpoint.BaseURL); err != nil {
				return err
			}
			if err := ValidatePromptAuditModel(endpoint.Model); err != nil {
				return err
			}
		}
		if _, err := ParsePromptAuditMaxConcurrency(strconv.Itoa(config.MaxConcurrency)); err != nil {
			return err
		}
		if _, err := ParsePromptAuditScanners(strings.Join(config.Scanners, ",")); err != nil {
			return err
		}
		return nil
	}
	if err := ValidatePromptAuditBaseURL(config.BaseURL); err != nil {
		return err
	}
	if err := ValidatePromptAuditModel(config.Model); err != nil {
		return err
	}
	if _, err := ParsePromptAuditTimeoutMS(strconv.Itoa(config.TimeoutMS)); err != nil {
		return err
	}
	if _, err := ParsePromptAuditInputLimit(strconv.Itoa(config.InputLimit)); err != nil {
		return err
	}
	if _, err := ParsePromptAuditMaxConcurrency(strconv.Itoa(config.MaxConcurrency)); err != nil {
		return err
	}
	if _, err := ParsePromptAuditScanners(strings.Join(config.Scanners, ",")); err != nil {
		return err
	}
	return nil
}
