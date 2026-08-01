package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountContentModerationViolationsCanExcludeCyberPolicy(t *testing.T) {
	usePromptAuditTestDB(t)
	require.NoError(t, DB.AutoMigrate(&ContentModerationLog{}))
	for _, action := range []string{"block", "cyber_policy", "hash_block"} {
		require.NoError(t, CreateContentModerationLog(t.Context(), &ContentModerationLog{
			CreatedAt: 100, UserId: 42, Flagged: true, Action: action,
		}))
	}
	count, err := CountContentModerationViolations(t.Context(), 42, 1, false)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	count, err = CountContentModerationViolations(t.Context(), 42, 1, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}
