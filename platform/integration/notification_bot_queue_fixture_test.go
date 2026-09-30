package integration_test

import (
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Drain actual bot lane heads so a fixture observes persisted card continuations.
// Domain heads are left to their own senders; this helper never bypasses Begin.
func deliverNotificationBotCards(t *testing.T, f *fixture) {
	t.Helper()
	pumpBotDeliveries(t, f.b)
}

func handleNotificationUpdate(t *testing.T, f *fixture, update telegram.Update) {
	t.Helper()
	handle(t, f.b, update)
	deliverNotificationBotCards(t, f)
}
