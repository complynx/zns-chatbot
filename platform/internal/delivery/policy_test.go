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

func TestUncertaintyRetryBasePreservesLegacySettingsAndCooldown(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{
		BotID: 42, BotInterval: time.Second, ChatInterval: time.Second,
		Fallback: 1500 * time.Millisecond,
	}
	require.NoError(t, settings.Validate())
	assert.Equal(t, 5*time.Second, settings.UncertaintyRetryBaseOrDefault())
	settings.UncertaintyRetryBase = 7 * time.Second
	require.NoError(t, settings.Validate())
	assert.Equal(t, 7*time.Second, settings.UncertaintyRetryBaseOrDefault())
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	deadline, valid := settings.Cooldown(now,
		delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", Missing: true})
	require.True(t, valid)
	assert.Equal(t, now.Add(2*time.Second), deadline, "missing 429 cooldown still uses its separate fallback")
	settings.UncertaintyRetryBase = -time.Second
	assert.ErrorIs(t, settings.Validate(), delivery.ErrSettings)
}
