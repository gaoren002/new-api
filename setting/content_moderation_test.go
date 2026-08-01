package setting

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationConfigDefaultsAndNormalization(t *testing.T) {
	config := DefaultContentModerationStorageConfig()
	config.BaseURL = " https://moderation.example/v1/ "
	config.Groups = []string{" paid ", "Paid", "default"}
	config.BlockedKeywords = []string{" blocked ", "BLOCKED"}
	config.SampleRate = 120
	NormalizeContentModerationStorageConfig(&config)
	require.Equal(t, "https://moderation.example/v1", config.BaseURL)
	require.Equal(t, 100, config.SampleRate)
	require.Equal(t, []string{"default", "paid"}, config.Groups)
	require.Equal(t, []string{"blocked"}, config.BlockedKeywords)
	require.Equal(t, DefaultCyberSessionBlockTTL, config.CyberSessionBlockTTLSeconds)
	require.NoError(t, ValidateContentModerationStorageConfig(config))
}

func TestContentModerationConfigValidation(t *testing.T) {
	config := DefaultContentModerationStorageConfig()
	config.BaseURL = "file:///tmp/moderation"
	require.Error(t, ValidateContentModerationStorageConfig(config))

	config = DefaultContentModerationStorageConfig()
	config.Mode = "invalid"
	require.Error(t, ValidateContentModerationStorageConfig(config))

	config = DefaultContentModerationStorageConfig()
	config.WorkerCount = MaxContentModerationWorkers + 1
	require.Error(t, ValidateContentModerationStorageConfig(config))

	config = DefaultContentModerationStorageConfig()
	config.ProxyURL = "file:///tmp/proxy"
	require.Error(t, ValidateContentModerationStorageConfig(config))

	config = DefaultContentModerationStorageConfig()
	config.CyberSessionBlockTTLSeconds = 30
	require.Error(t, ValidateContentModerationStorageConfig(config))
}

func TestContentModerationStorageRejectsTrailingJSON(t *testing.T) {
	previous := ContentModerationConfigJSON
	t.Cleanup(func() { ContentModerationConfigJSON = previous })
	raw, err := json.Marshal(DefaultContentModerationStorageConfig())
	require.NoError(t, err)
	ContentModerationConfigJSON = string(raw) + ` {"enabled":true}`
	_, err = GetContentModerationStorageConfig()
	require.ErrorContains(t, err, "trailing JSON")
}

func TestContentModerationActiveRequiresEnabledNonOffMode(t *testing.T) {
	config := DefaultContentModerationStorageConfig()
	require.False(t, ContentModerationStorageActive(config))
	require.False(t, ContentModerationCyberPolicyActive(config))
	config.Enabled = true
	require.True(t, ContentModerationStorageActive(config))
	require.True(t, ContentModerationCyberPolicyActive(config))
	config.Mode = ContentModerationModeOff
	require.False(t, ContentModerationStorageActive(config))
	require.True(t, ContentModerationCyberPolicyActive(config))

	raw, err := json.Marshal(config)
	require.NoError(t, err)
	require.False(t, ContentModerationJSONActive(string(raw)))
	require.False(t, ContentModerationJSONActive(strings.Repeat("x", 10)))
}
