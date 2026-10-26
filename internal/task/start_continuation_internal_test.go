package task

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStartPayloadNormalization(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"/start /profile", " /start\tbook a massage ", "/start\n/register event"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			u := telegram.Update{ID: 1, Message: &telegram.Message{ID: 1,
				From: &telegram.User{ID: 42}, Chat: telegram.Chat{ID: 42, Type: "private"}, Text: text}}
			raw, err := json.Marshal(u)
			require.NoError(t, err)
			i, err := normalize(u, raw)
			require.NoError(t, err)
			require.Equal(t, kindStructured, i.Kind)
			require.Equal(t, startCommand, i.StructuredCommand)
		})
	}
	for _, text := range []string{"/start", "/start ", "/starter text", "start text"} {
		require.Empty(t, startPayload(text))
	}
}

func TestStartPayloadUsesNormalCommandClassification(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{"/profile", "/register event", "/unknown request", "book a massage", "/name"} {
		i := intent{StartContinuation: payload}
		applyStartPayload(&i, payload)
		require.Equal(t, payload, i.Text)
		require.Empty(t, i.StartContinuation)
		if structuredCommand(payload) {
			require.Equal(t, kindStructured, i.Kind)
		} else {
			require.Equal(t, kindMessage, i.Kind)
		}
	}
}

func TestStartPayloadPreservesPartnerDeepLink(t *testing.T) {
	t.Parallel()
	text := "/start partner_0123456789abcdef0123456789abcdef"
	u := telegram.Update{ID: 1, Message: &telegram.Message{ID: 1,
		From: &telegram.User{ID: 42}, Chat: telegram.Chat{ID: 42, Type: "private"}, Text: text}}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	i, err := normalize(u, raw)
	require.NoError(t, err)
	require.NotNil(t, i.PartnerCode)
	require.Equal(t, kindMessage, i.Kind)
	require.Empty(t, i.StartContinuation)
}
