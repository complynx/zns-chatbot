package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type adminDeliveryProvider struct {
	token string
	err   error
	calls int
}

func (*adminDeliveryProvider) Telegram(context.Context, int64) (identity.User, error) {
	return identity.User{Owner: "bob", Subject: "subject-bob"}, nil
}
func (p *adminDeliveryProvider) Exchange(context.Context, string) (string, error) {
	p.calls++
	return p.token, p.err
}

func TestAdminMessageProviderDeliveryBoundary(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"inactive", "invalid_token", "outage"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service := adminmessage.Service{DB: f.db}
			preview, err := service.PreviewCommand(
				t.Context(),
				"bob",
				"provider-boundary",
				`/send_message_to 101 --msg "Queued provider check"`,
			)
			require.NoError(t, err)
			require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
			provider := &adminDeliveryProvider{
				token: f.b.Host.Signer.Token("bob"),
				err:   identity.ErrZitadelUserInactive,
			}
			if scenario == "outage" {
				provider.err = errors.New("temporary provider outage")
			}
			if scenario == "invalid_token" {
				provider.err = nil
				provider.token = "invalid-provider-token"
			}
			f.b.API.Links, f.b.API.Exchange = provider, provider
			f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: f.model}
			err = f.b.DeliverAdminMessages(t.Context())
			if scenario == "outage" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, chatMessages(t, f, 101), "provider rejection must prevent Telegram send")
			require.Equal(t, 1, provider.calls)
			results, err := service.Results(t.Context(), "bob", preview.ID)
			require.NoError(t, err)
			require.Len(t, results, 1)
			if scenario != "outage" {
				require.Equal(t, "failed", results[0].State)
				require.Equal(t, "admin_identity_denied", results[0].Failure)
				provider.err = nil
				require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
				require.Empty(t, chatMessages(t, f, 101))
				return
			}
			require.Equal(t, "pending", results[0].State)
			require.Equal(t, "admin_identity_unavailable", results[0].Failure)
			provider.err = nil
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp() WHERE message_id=$1`,
				preview.ID,
			)
			require.NoError(t, err)
			f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: f.model}
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			require.Len(t, chatMessages(t, f, 101), 1)
			require.Equal(t, 2, provider.calls)
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			require.Len(t, chatMessages(t, f, 101), 1)
		})
	}
}
