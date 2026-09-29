package delivery_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestCooldownPreservesRepresentableLargeDelay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 0, 0, 0, 123, time.UTC)
	seconds := int64(20000000000)
	deadline, ok := delivery.Deadline(now, seconds)
	require.True(t, ok)
	assert.Equal(t, seconds, deadline.Unix()-now.Unix())
	assert.Equal(t, now.Nanosecond(), deadline.Nanosecond())
	_, ok = delivery.Deadline(now, math.MaxInt64)
	assert.False(t, ok)
	_, ok = delivery.Deadline(now, -1)
	assert.False(t, ok)
}

func TestMissingCooldownUsesRoundedConfiguredFallback(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{
		BotID:        42,
		BotInterval:  time.Second,
		ChatInterval: time.Second,
		Fallback:     1500 * time.Millisecond,
	}
	require.NoError(t, settings.Validate())
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	deadline, ok := settings.Cooldown(
		now,
		delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", Missing: true},
	)
	require.True(t, ok)
	assert.Equal(t, now.Add(2*time.Second), deadline)
	settings.BotID = 0
	assert.ErrorIs(t, settings.Validate(), delivery.ErrSettings)
}
