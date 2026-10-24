package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestAvatarBrowserPolicyDefaultsAndSmallerBudgets(t *testing.T) {
	t.Parallel()
	policy, err := (db.Settings{}).AvatarBrowserPolicy()
	require.NoError(t, err)
	require.Equal(t, int64(10_000_000_000), policy.LimitBytes)
	require.Equal(t, 90*24*time.Hour, policy.Retention)
	policy, err = (db.Settings{
		AvatarBrowserStorageLimitBytes: 1000,
		AvatarBrowserRetentionSeconds:  60,
	}).AvatarBrowserPolicy()
	require.NoError(t, err)
	require.Equal(t, int64(1000), policy.LimitBytes)
	require.Equal(t, time.Minute, policy.Retention)
	for _, settings := range []db.Settings{
		{AvatarBrowserStorageLimitBytes: -1}, {AvatarBrowserStorageLimitBytes: 10_000_000_001},
		{AvatarBrowserRetentionSeconds: -1}, {AvatarBrowserRetentionSeconds: 59},
		{AvatarBrowserRetentionSeconds: 7_776_001},
	} {
		_, err = settings.AvatarBrowserPolicy()
		require.Error(t, err)
	}
}
