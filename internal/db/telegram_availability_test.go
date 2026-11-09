package db_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestTelegramAvailabilityMigrationPreservesSuspensionOnRollback(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	provider := baselineProvider(t, store)
	require.NoError(t, store.SuspendTelegram(t.Context(), 1, 42, 0, "user_deactivated"))
	reopened, err := db.Open(t.Context(), store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	require.ErrorIs(t, reopened.CheckTelegramAvailability(t.Context(), 1, 42), db.ErrTelegramUnavailable)
	_, err = provider.DownTo(t.Context(), 78)
	require.ErrorContains(t, err, "Telegram suspensions must be preserved")
	require.ErrorIs(t, store.CheckTelegramAvailability(t.Context(), 1, 42), db.ErrTelegramUnavailable)
	_, _, err = store.Intake(t.Context(), db.IntakeRequest{BotID: 1, UpdateID: 1, TelegramUserID: 42, ChatID: 42,
		Payload: json.RawMessage(`{}`)}, nil)
	require.NoError(t, err)
	_, err = provider.DownTo(t.Context(), 78)
	require.NoError(t, err)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.CheckTelegramAvailability(t.Context(), 1, 42))
}
