package integration_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
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
			service := configureDeliveryFixture(t, f)
			preview, err := service.PreviewCommand(
				t.Context(),
				"bob",
				"provider-boundary",
				`/send_message_to 101 --msg "Queued provider check"`,
			)
			require.NoError(t, err)
			require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
			initial, initialErr := service.Results(t.Context(), "bob", preview.ID)
			require.NoError(t, initialErr)
			require.Len(t, initial, 1)
			waitAdminDeliveryCandidate(t, f, initial[0].ID, time.Second)
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
			f.b = &bot.Bot{
				DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG,
				Model: f.model, Delivery: f.b.Delivery,
			}
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
				require.Equal(t, string(delivery.Cancelled), results[0].State)
				require.Equal(t, "admin_identity_denied", results[0].Failure)
				provider.err = nil
				provider.token = f.b.Host.Signer.Token("bob")
				require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
				require.Empty(t, chatMessages(t, f, 101))
				require.Equal(t, 1, provider.calls)
				return
			}
			require.Equal(t, "pending", results[0].State)
			require.Equal(t, "admin_identity_unavailable", results[0].Failure)
			provider.err = nil
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			require.Empty(t, chatMessages(t, f, 101), "restoring identity does not bypass retry pacing")
			require.Equal(t, 1, provider.calls)
			var availableAt time.Time
			require.NoError(t, f.db.QueryRow(t.Context(),
				"SELECT available_at FROM core.admin_message_deliveries WHERE id=$1",
				results[0].ID).Scan(&availableAt))
			require.True(t, availableAt.After(time.Now()))
			waitAdminDeliveryCandidate(t, f, results[0].ID, f.b.Delivery.Fallback+5*time.Second)
			f.b = &bot.Bot{
				DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG,
				Model: f.model, Delivery: f.b.Delivery,
			}
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			sent := chatMessages(t, f, 101)
			require.Len(t, sent, 1)
			require.Equal(t, "Queued provider check", sent[0].Text)
			completed, resultErr := service.Results(t.Context(), "bob", preview.ID)
			require.NoError(t, resultErr)
			require.Len(t, completed, 1)
			require.Equal(t, results[0].ID, completed[0].ID)
			require.Equal(t, "sent", completed[0].State)
			require.Equal(t, sent[0].ID, completed[0].TelegramMessageID)
			require.Equal(t, 2, provider.calls)
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			require.Len(t, chatMessages(t, f, 101), 1)
		})
	}
}

// waitAdminDeliveryCandidate observes the real queue deadline without changing it.
func waitAdminDeliveryCandidate(t *testing.T, f *fixture, id int64, timeout time.Duration) {
	t.Helper()
	ref := delivery.Reference{Owner: delivery.Admin, Key: strconv.FormatInt(id, 10), Effect: "send"}
	require.Eventually(t, func() bool {
		for _, entry := range botDeliveryCandidates(t, f.b) {
			if entry.Reference == ref {
				return true
			}
		}
		return false
	}, timeout, 20*time.Millisecond)
}
