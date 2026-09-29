package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestOnboardingOnlyAcceptedInboundUpdates(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"message", "callback", "group", "mismatch", "bot", "zero", "lookupfailure", "recipient"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			b := Bot{
				API: appclient.Client{
					Links:        authLinks{err: identity.ErrZitadelIdentity},
					Exchange:     &authExchange{},
					SandboxToken: (identity.Signer{}).Token,
				},
			}
			calls := 0
			b.Onboarding = func(_ context.Context, u telegram.User) error {
				calls++
				require.Equal(t, int64(101), u.ID)
				b.API.Links = authLinks{user: identity.User{Owner: "created", Subject: "reserved"}}
				return nil
			}
			u := telegram.Update{
				Message: &telegram.Message{From: telegram.User{ID: 101}, Chat: telegram.Chat{ID: 101, Type: "private"}},
			}
			switch kind {
			case "callback":
				u.Callback = &telegram.Callback{From: u.Message.From, Message: *u.Message}
				u.Message = nil
			case "group":
				u.Message.Chat.Type = "group"
			case "mismatch":
				u.Message.Chat.ID = 202
			case "bot":
				u.Message.From.IsBot = true
			case "zero":
				u.Message.From.ID = 0
				u.Message.Chat.ID = 0
			case "lookupfailure":
				b.API.Links = authLinks{err: errors.New("database failure")}
			case "recipient":
				_, err := b.API.NotificationContext(t.Context(), "created", 101)
				require.Error(t, err)
				require.Zero(t, calls)
				return
			}
			_, in, accepted, err := b.authenticatedUpdate(t.Context(), u)
			if kind == "message" || kind == "callback" {
				require.NoError(t, err)
				require.True(t, accepted)
				require.Equal(t, "created", in.owner)
				require.Equal(t, 1, calls)
			} else {
				require.False(t, accepted)
				require.Zero(t, calls)
			}
		})
	}
}
