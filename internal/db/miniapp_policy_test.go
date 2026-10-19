package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMiniAppSettingsPreserveDefaultsAndRejectOutOfBounds(t *testing.T) {
	t.Parallel()
	policy, err := (db.Settings{}).MiniAppPolicy()
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, policy.MaxAge)
	require.Equal(t, 2*time.Minute, policy.PendingTTL)
	require.Equal(t, 15*time.Minute, policy.SessionTTL)
	for _, settings := range []db.Settings{{MiniAppMaxAgeSeconds: -1}, {MiniAppMaxAgeSeconds: 901},
		{MiniAppPendingSeconds: 29}, {MiniAppPendingSeconds: 301}, {MiniAppSessionSeconds: 59}, {MiniAppSessionSeconds: 3601}} {
		_, err = settings.MiniAppPolicy()
		require.Error(t, err)
	}
	policy, err = (db.Settings{MiniAppMaxAgeSeconds: 30, MiniAppPendingSeconds: 30, MiniAppSessionSeconds: 60}).MiniAppPolicy()
	require.NoError(t, err)
	require.Equal(t, time.Minute, policy.SessionTTL)
}
