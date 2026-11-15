package broadcasts_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
)

func TestPersonalizedLinksCannotAddPrivateData(t *testing.T) {
	t.Parallel()
	original := "Details: https://event.example/r?n= and t.me/event, contact @event_host."
	for _, rendered := range []string{
		"Подробности: https://event.example/r?n= и t.me/event, контакт @event_host.",
		"See https://event.example/r?n=.",
		"Встречаемся в 18:00.",
		"Встречаемся в 18.00, цена 1.500 ₽, дата 20.11.2026.",
	} {
		require.True(t, broadcasts.PreservesLinks(original, rendered), rendered)
	}
	for _, rendered := range []string{
		"https://event.example/r?n=private-memory",
		"https://attacker.example/private",
		"attacker.example/?secret=memory",
		"t.me/another_account",
		"@another_account",
		"mailto:private@example.org",
		"private@example.org",
		"http://127.0.0.1/private",
		"127.0.0.1/private",
	} {
		require.False(t, broadcasts.PreservesLinks(original, rendered), rendered)
	}
}
