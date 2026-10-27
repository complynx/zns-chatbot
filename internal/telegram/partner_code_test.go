package telegram_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPartnerCodeRoundTripsOnlyThroughVerifiedBotURL(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("d3ddf61a-79f9-4c50-81f2-94e346d077ec")
	bot := telegram.User{ID: 123, IsBot: true, Username: "zinusya_test_bot"}
	link := telegram.PartnerShareURL(bot, 123, id)
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "t.me", parsed.Host)
	require.Equal(t, "/zinusya_test_bot", parsed.Path)
	require.Len(t, parsed.Query().Get("start"), 40)
	require.Equal(t, &id, telegram.PartnerStartCode("/start "+parsed.Query().Get("start")))
	for _, invalid := range []telegram.User{
		{ID: 999, IsBot: true, Username: bot.Username}, {ID: 123, Username: bot.Username},
		{ID: 123, IsBot: true, Username: "other_bot?start=injected"},
		{ID: 123, IsBot: true, Username: strings.Repeat("x", 33)},
	} {
		require.Empty(t, telegram.PartnerShareURL(invalid, 123, id))
	}
	require.Empty(t, telegram.PartnerShareURL(bot, 123, uuid.Nil))
}

func TestPartnerCodeDoesNotTreatArbitraryTextAsEntry(t *testing.T) {
	t.Parallel()
	code := "partner_d3ddf61a79f94c5081f294e346d077ec"
	for _, text := range []string{
		"/start", "/start " + code + " extra", " /start " + code, "/start " + code + "\n",
		"/start@zinusya_test_bot " + code, "/start " + strings.ToUpper(code),
		"/start partner_00000000000000000000000000000000", "/start partner_invalid",
		"/start partner_d3ddf61a-79f9-4c50-81f2-94e346d077ec", "Someone said /start " + code,
	} {
		require.Nil(t, telegram.PartnerStartCode(text), text)
	}
}
