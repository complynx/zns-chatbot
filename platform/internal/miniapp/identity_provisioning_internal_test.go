package miniapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type unknownLinks struct{}

func (unknownLinks) Telegram(context.Context, int64) (identity.User, error) {
	return identity.User{}, identity.ErrZitadelIdentity
}

type unusedExchange struct{}

func (unusedExchange) Exchange(context.Context, string) (string, error) { return "unused", nil }

func TestMiniAppOnboardsOnlyVerifiedTelegramInput(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"valid", "invalid", "cookie", "bot"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			calls := 0
			g := Gateway{
				Token: "synthetic-bot",
				API: appclient.Client{
					Links:        unknownLinks{},
					Exchange:     unusedExchange{},
					SandboxToken: (identity.Signer{}).Token,
				},
				Onboarding: func(_ context.Context, u telegram.User) error {
					calls++
					require.Equal(t, int64(95100), u.ID)
					return nil
				},
			}
			u := telegram.User{ID: 95100, FirstName: "Synthetic"}
			if kind == "bot" {
				u.IsBot = true
			}
			raw := sandbox.WebAppInitData(u, g.Token, time.Now())
			if kind == "invalid" {
				raw += "tampered"
			}
			r := httptest.NewRequest(http.MethodGet, "/miniapp/api/quote", nil)
			if kind != "cookie" {
				r.Header.Set("Authorization", "tma "+raw)
			} else {
				r.AddCookie(&http.Cookie{Name: "untrusted", Value: "95100"})
			}
			id, err := g.authenticate(r)
			if kind == "valid" {
				require.NoError(t, err)
				require.Equal(t, u.ID, id)
				require.Equal(t, 1, calls)
			} else {
				require.Error(t, err)
				require.Zero(t, calls)
			}
		})
	}
}
