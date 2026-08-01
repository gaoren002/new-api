package model

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestSecurityAuditConfigsAreMutuallyExclusive(t *testing.T) {
	usePromptAuditTestDB(t)
	previousPrompt := setting.PromptAuditConfigJSON
	previousContent := setting.ContentModerationConfigJSON
	previousLegacyPrompt := setting.PromptAuditEnabled
	setting.PromptAuditConfigJSON = ""
	setting.ContentModerationConfigJSON = ""
	setting.PromptAuditEnabled = false
	t.Cleanup(func() {
		setting.PromptAuditConfigJSON = previousPrompt
		setting.ContentModerationConfigJSON = previousContent
		setting.PromptAuditEnabled = previousLegacyPrompt
	})

	prompt := setting.DefaultPromptAuditStorageConfig()
	prompt.Mode = setting.PromptAuditModeBlocking
	prompt.ConfigVersion = 2
	promptRaw, err := json.Marshal(prompt)
	require.NoError(t, err)
	require.NoError(t, UpdateSecurityAuditOptionCAS(setting.PromptAuditConfigOptionKey, 1, string(promptRaw)))

	content := setting.DefaultContentModerationStorageConfig()
	content.Enabled = true
	content.Mode = setting.ContentModerationModePreBlock
	content.ConfigVersion = 2
	contentRaw, err := json.Marshal(content)
	require.NoError(t, err)
	require.ErrorIs(t, UpdateSecurityAuditOptionCAS(setting.ContentModerationConfigOptionKey, 1, string(contentRaw)), ErrSecurityAuditEngineConflict)

	prompt.Mode = setting.PromptAuditModeOff
	prompt.ConfigVersion = 3
	promptRaw, err = json.Marshal(prompt)
	require.NoError(t, err)
	require.NoError(t, UpdateSecurityAuditOptionCAS(setting.PromptAuditConfigOptionKey, 2, string(promptRaw)))
	require.NoError(t, UpdateSecurityAuditOptionCAS(setting.ContentModerationConfigOptionKey, 1, string(contentRaw)))
}

func TestSecurityAuditCASRejectsStaleVersion(t *testing.T) {
	usePromptAuditTestDB(t)
	previous := setting.ContentModerationConfigJSON
	setting.ContentModerationConfigJSON = ""
	t.Cleanup(func() { setting.ContentModerationConfigJSON = previous })
	config := setting.DefaultContentModerationStorageConfig()
	config.ConfigVersion = 2
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, UpdateSecurityAuditOptionCAS(setting.ContentModerationConfigOptionKey, 1, string(raw)))
	require.ErrorIs(t, UpdateSecurityAuditOptionCAS(setting.ContentModerationConfigOptionKey, 1, string(raw)), ErrOptionVersionConflict)
}

func TestSecurityAuditConcurrentEnableCommitsAtMostOneEngine(t *testing.T) {
	usePromptAuditTestDB(t)
	previousPrompt := setting.PromptAuditConfigJSON
	previousContent := setting.ContentModerationConfigJSON
	previousLegacyPrompt := setting.PromptAuditEnabled
	setting.PromptAuditConfigJSON = ""
	setting.ContentModerationConfigJSON = ""
	setting.PromptAuditEnabled = false
	t.Cleanup(func() {
		setting.PromptAuditConfigJSON = previousPrompt
		setting.ContentModerationConfigJSON = previousContent
		setting.PromptAuditEnabled = previousLegacyPrompt
	})

	prompt := setting.DefaultPromptAuditStorageConfig()
	prompt.Mode = setting.PromptAuditModeBlocking
	prompt.ConfigVersion = 2
	promptRaw, err := json.Marshal(prompt)
	require.NoError(t, err)
	content := setting.DefaultContentModerationStorageConfig()
	content.Enabled = true
	content.ConfigVersion = 2
	contentRaw, err := json.Marshal(content)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for _, update := range []struct {
		key string
		raw string
	}{
		{setting.PromptAuditConfigOptionKey, string(promptRaw)},
		{setting.ContentModerationConfigOptionKey, string(contentRaw)},
	} {
		waitGroup.Add(1)
		go func(key, raw string) {
			defer waitGroup.Done()
			<-start
			results <- UpdateSecurityAuditOptionCAS(key, 1, raw)
		}(update.key, update.raw)
	}
	close(start)
	waitGroup.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)

	var promptOption, contentOption Option
	_ = DB.Where(commonKeyCol+" = ?", setting.PromptAuditConfigOptionKey).First(&promptOption).Error
	_ = DB.Where(commonKeyCol+" = ?", setting.ContentModerationConfigOptionKey).First(&contentOption).Error
	require.False(t, setting.PromptAuditJSONActive(promptOption.Value) && setting.ContentModerationJSONActive(contentOption.Value))
}
