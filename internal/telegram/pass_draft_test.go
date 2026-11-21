package telegram_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPassDraftURLPrefillsWithoutSubmitting(t *testing.T) {
	t.Parallel()
	const command = "/start /passes summer-zouk --invite BADGE4"
	bot := telegram.User{ID: 42, IsBot: true, Username: "SummerEventBot"}
	link, err := url.Parse(telegram.PassDraftURL(bot, 42, command))
	require.NoError(t, err)
	require.Equal(t, "t.me", link.Host)
	require.Equal(t, "/SummerEventBot", link.Path)
	require.Equal(t, command, link.Query().Get("text"))
	require.NotContains(t, link.RawQuery, "+")
	require.Empty(t, link.Query().Get("start"))
	require.Empty(t, telegram.PassDraftURL(bot, 43, command))
}
