package telegram_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestWebAppIdentityRequiresFreshSignedHuman(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	const token = "test-bot-token"
	user := telegram.User{ID: 101, FirstName: "Alice"}
	valid := sandbox.WebAppInitData(user, token, now)
	verified, err := telegram.VerifyWebApp(valid, token, now)
	require.NoError(t, err)
	assert.Equal(t, user, verified)
	values, err := url.ParseQuery(valid)
	require.NoError(t, err)
	values.Set("user", `{"id":202,"first_name":"Boris"}`)
	for name, raw := range map[string]string{
		"tampered user":  values.Encode(),
		"duplicate user": valid + "&user=%7B%22id%22%3A202%7D",
		"duplicate hash": valid + "&hash=00",
		"wrong token":    sandbox.WebAppInitData(user, "other-bot", now),
		"expired":        sandbox.WebAppInitData(user, token, now.Add(-2*time.Hour)),
		"future":         sandbox.WebAppInitData(user, token, now.Add(time.Minute)),
		"bot":            sandbox.WebAppInitData(telegram.User{ID: 101, IsBot: true}, token, now),
		"missing id":     sandbox.WebAppInitData(telegram.User{}, token, now),
		"malformed":      "hash=%ZZ",
		"oversized":      strings.Repeat("x", 16385),
		"unsigned":       `auth_date=123&user={"id":101}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, verifyError := telegram.VerifyWebApp(raw, token, now)
			require.Error(t, verifyError)
		})
	}
}
