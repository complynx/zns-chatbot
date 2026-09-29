package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Drain actual bot lane heads so a fixture observes persisted card continuations.
// Domain heads are left to their own senders; this helper never bypasses Begin.
func deliverNotificationBotCards(t *testing.T, f *fixture) {
	t.Helper()
	for range 32 {
		tx, err := f.db.Begin(t.Context())
		require.NoError(t, err)
		entries, err := delivery.Candidates(t.Context(), tx, f.b.Delivery.BotID, 100)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(t.Context()))
		delivered := false
		for _, entry := range entries {
			if entry.Reference.Owner != delivery.Bot {
				continue
			}
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), entry.Reference))
			delivered = true
		}
		if !delivered {
			return
		}
	}
	t.Fatal("bot card fixture did not settle within 32 dispatch passes")
}

func handleNotificationUpdate(t *testing.T, f *fixture, update telegram.Update) {
	t.Helper()
	handle(t, f.b, update)
	deliverNotificationBotCards(t, f)
}
