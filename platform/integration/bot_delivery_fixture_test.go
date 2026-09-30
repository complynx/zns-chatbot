package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// handleVisible admits an update, then drives its available presentation work.
// Use handle or Bot.Handle when asserting the enqueue boundary itself.
func handleVisible(t *testing.T, b *bot.Bot, update telegram.Update) {
	t.Helper()
	handle(t, b, update)
	pumpBotDeliveries(t, b)
}

// pumpBotDeliveries uses real lane heads and owner dispatch outside selection
// transactions. It leaves domain heads and failed attempts for explicit tests.
func pumpBotDeliveries(t *testing.T, b *bot.Bot) {
	t.Helper()
	for range 100 {
		entries := botDeliveryCandidates(t, b)
		if len(entries) == 0 {
			// Wait only ordinary fixture pacing, never a provider retry deadline.
			delay := max(b.Delivery.BotInterval, b.Delivery.ChatInterval)
			if delay > 100*time.Millisecond {
				return
			}
			timer := time.NewTimer(delay + time.Millisecond)
			select {
			case <-timer.C:
			case <-t.Context().Done():
				timer.Stop()
				t.Fatal(t.Context().Err())
			}
			entries = botDeliveryCandidates(t, b)
		}
		found := false
		for _, entry := range entries {
			if entry.Reference.Owner != delivery.Bot {
				continue
			}
			found = true
			require.NoError(t, b.DeliverBotIntent(t.Context(), entry.Reference))
			intent, err := botdelivery.Read(t.Context(), b.DB, b.Delivery.BotID, entry.Reference, false)
			require.NoError(t, err)
			if intent.State != delivery.Succeeded && intent.State != delivery.Cancelled {
				return
			}
			// Success and pre-send cancellation both release the lane head.
			break
		}
		if !found {
			return
		}
	}
	t.Fatal("bot presentation did not settle within 100 dispatches")
}

func botDeliveryCandidates(t *testing.T, b *bot.Bot) []delivery.Entry {
	t.Helper()
	tx, err := b.DB.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	entries, err := delivery.Candidates(t.Context(), tx, b.Delivery.BotID, 100)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return entries
}
