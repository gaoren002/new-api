package setting

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePromptAuditBaseURL(t *testing.T) {
	require.NoError(t, ValidatePromptAuditBaseURL("http://qwen3guard:11434"))
	require.NoError(t, ValidatePromptAuditBaseURL("https://guard.example/openai/v1"))
	for _, value := range []string{"", "guard.example/v1", "ftp://guard.example", "https://key@guard.example", "https://guard.example?token=x"} {
		require.Error(t, ValidatePromptAuditBaseURL(value))
	}
}

func TestPromptAuditStorageConfigModesEndpointsAndGroups(t *testing.T) {
	previous := PromptAuditConfigJSON
	defer func() { PromptAuditConfigJSON = previous }()
	storage := DefaultPromptAuditStorageConfig()
	storage.Mode = PromptAuditModeAsync
	storage.FailClosed = false
	storage.ConfigVersion = 7
	storage.Endpoints = append(storage.Endpoints, PromptAuditEndpoint{
		ID: "backup", Name: "Backup", Protocol: "openai_compatible", BaseURL: "https://guard.example/v1",
		Model: "guard", TimeoutMS: 2000, InputLimit: 2048, Enabled: true,
	})
	storage.GroupPolicies = map[string]PromptAuditGroupPolicy{
		"strict": {Mode: PromptAuditModeBlocking, Enabled: true, FailClosed: false, Scanners: []string{"pii"}},
		"off":    {Mode: PromptAuditModeOff, Enabled: false, Scanners: []string{"jailbreak"}},
	}
	raw, err := json.Marshal(storage)
	require.NoError(t, err)
	PromptAuditConfigJSON = string(raw)
	loaded, err := GetPromptAuditStorageConfig()
	require.NoError(t, err)
	require.True(t, loaded.FailClosed)
	require.True(t, loaded.GroupPolicies["strict"].FailClosed)
	require.Equal(t, PromptAuditModeAsync, GetPromptAuditConfigForGroup("default").Mode)
	strict := GetPromptAuditConfigForGroup("strict")
	require.Equal(t, PromptAuditModeBlocking, strict.Mode)
	require.True(t, strict.FailClosed)
	require.Equal(t, []string{"pii"}, strict.Scanners)
	require.False(t, GetPromptAuditConfigForGroup("off").Enabled)
	require.Len(t, strict.Endpoints, 2)
}

func TestPromptAuditTokenEncryptionRoundTripAndFallbackKey(t *testing.T) {
	primary, hadPrimary := os.LookupEnv(PromptAuditEncryptionKeyEnv)
	fallback, hadFallback := os.LookupEnv(PromptAuditFallbackKeyEnv)
	t.Cleanup(func() {
		if hadPrimary {
			_ = os.Setenv(PromptAuditEncryptionKeyEnv, primary)
		} else {
			_ = os.Unsetenv(PromptAuditEncryptionKeyEnv)
		}
		if hadFallback {
			_ = os.Setenv(PromptAuditFallbackKeyEnv, fallback)
		} else {
			_ = os.Unsetenv(PromptAuditFallbackKeyEnv)
		}
	})
	require.NoError(t, os.Unsetenv(PromptAuditEncryptionKeyEnv))
	require.NoError(t, os.Setenv(PromptAuditFallbackKeyEnv, strings.Repeat("42", 32)))
	ciphertext, err := EncryptPromptAuditToken("guard-secret")
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "guard-secret")
	plaintext, err := DecryptPromptAuditToken(ciphertext)
	require.NoError(t, err)
	require.Equal(t, "guard-secret", plaintext)
}

func TestParsePromptAuditNumericSettings(t *testing.T) {
	timeout, err := ParsePromptAuditTimeoutMS("10000")
	require.NoError(t, err)
	require.Equal(t, 10000, timeout)
	inputLimit, err := ParsePromptAuditInputLimit("8000")
	require.NoError(t, err)
	require.Equal(t, 8000, inputLimit)
	maxConcurrency, err := ParsePromptAuditMaxConcurrency("4")
	require.NoError(t, err)
	require.Equal(t, 4, maxConcurrency)
	inputLimit, err = ParsePromptAuditInputLimit("2000000")
	require.NoError(t, err)
	require.Equal(t, 2000000, inputLimit)
	_, err = ParsePromptAuditInputLimit("2000001")
	require.EqualError(t, err, "prompt audit input limit must be between 1 and 2000000")
	for _, value := range []string{"0", "-1", "abc", "120001"} {
		_, err := ParsePromptAuditTimeoutMS(value)
		require.Error(t, err)
	}
}

func TestValidatePromptAuditStorageConfigResourceLimits(t *testing.T) {
	storage := DefaultPromptAuditStorageConfig()
	storage.WorkerCount = 64
	storage.Endpoints[0].InputLimit = 2000000
	require.NoError(t, ValidatePromptAuditStorageConfig(storage))

	storage.WorkerCount = 65
	require.EqualError(t, ValidatePromptAuditStorageConfig(storage), "prompt audit worker count must be between 1 and 64")

	storage.WorkerCount = 64
	storage.Endpoints[0].InputLimit = 2000001
	require.EqualError(t, ValidatePromptAuditStorageConfig(storage), "prompt audit endpoint input limit must be between 128 and 2000000")
}

func TestParsePromptAuditScanners(t *testing.T) {
	scanners, err := ParsePromptAuditScanners("jailbreak,pii,jailbreak")
	require.NoError(t, err)
	require.Equal(t, []string{"jailbreak", "pii"}, scanners)
	_, err = ParsePromptAuditScanners("")
	require.Error(t, err)
	_, err = ParsePromptAuditScanners("jailbreak,unknown")
	require.Error(t, err)
	require.Len(t, strings.Split(PromptAuditScanners, ","), len(PromptAuditScannerIDs))
}

func TestParsePromptAuditGroupPolicies(t *testing.T) {
	policies, err := ParsePromptAuditGroupPolicies(`{
		"codex-pro":{"enabled":true,"fail_closed":true,"scanners":["pii","jailbreak","pii"]},
		"default":{"enabled":false,"fail_closed":false,"scanners":["jailbreak"]}
	}`)
	require.NoError(t, err)
	require.Equal(t, []string{"pii", "jailbreak"}, policies["codex-pro"].Scanners)
	require.False(t, policies["default"].Enabled)

	for _, value := range []string{
		`{"":{"enabled":true,"fail_closed":false,"scanners":["pii"]}}`,
		`{" codex":{"enabled":true,"fail_closed":false,"scanners":["pii"]}}`,
		`{"codex":{"enabled":true,"fail_closed":false,"scanners":[]}}`,
		`{"codex":{"enabled":true,"fail_closed":false,"scanners":["unknown"]}}`,
		`{"codex":{"enabled":true,"fail_closed":false,"scanners":["pii"],"unknown":true}}`,
		`{} {}`,
		`null`,
	} {
		_, err := ParsePromptAuditGroupPolicies(value)
		require.Error(t, err, value)
	}
}

func TestGetPromptAuditConfigForGroup(t *testing.T) {
	previous := GetPromptAuditConfig()
	previousPolicies := PromptAuditGroupPolicies
	previousConfigJSON := PromptAuditConfigJSON
	defer func() {
		PromptAuditEnabled = previous.Enabled
		PromptAuditFailClosed = previous.FailClosed
		PromptAuditScanners = strings.Join(previous.Scanners, ",")
		PromptAuditGroupPolicies = previousPolicies
		PromptAuditConfigJSON = previousConfigJSON
	}()

	PromptAuditConfigJSON = ""
	PromptAuditEnabled = true
	PromptAuditFailClosed = false
	PromptAuditScanners = "jailbreak"
	PromptAuditGroupPolicies = `{"codex-pro":{"enabled":true,"fail_closed":true,"scanners":["pii"]},"free":{"enabled":false,"fail_closed":false,"scanners":["jailbreak"]}}`

	strict := GetPromptAuditConfigForGroup("codex-pro")
	require.True(t, strict.Enabled)
	require.True(t, strict.FailClosed)
	require.Equal(t, []string{"pii"}, strict.Scanners)
	require.False(t, GetPromptAuditConfigForGroup("free").Enabled)
	require.Equal(t, []string{"jailbreak"}, GetPromptAuditConfigForGroup("other").Scanners)

	PromptAuditEnabled = false
	strict = GetPromptAuditConfigForGroup("codex-pro")
	require.True(t, strict.Enabled)
	require.Equal(t, PromptAuditModeBlocking, strict.Mode)
}

func TestValidatePromptAuditStorageConfigRequiresEndpointForGroupOverride(t *testing.T) {
	storage := DefaultPromptAuditStorageConfig()
	storage.Mode = PromptAuditModeOff
	storage.Endpoints[0].Enabled = false
	storage.GroupPolicies["audited"] = PromptAuditGroupPolicy{
		Mode: PromptAuditModeBlocking, Enabled: true, Scanners: []string{"pii"},
	}
	require.ErrorContains(t, ValidatePromptAuditStorageConfig(storage), "at least one prompt audit endpoint")
}
