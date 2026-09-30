package integration_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

type unavailableIdentity struct {
	links  identity.Links
	failed atomic.Bool
}

func (l *unavailableIdentity) Telegram(ctx context.Context, sender int64) (identity.User, error) {
	if l.failed.Load() {
		return identity.User{}, errors.New("temporary identity database outage")
	}
	return l.links.Telegram(ctx, sender)
}

type deniedAlice struct{ denied atomic.Int64 }

func (p *deniedAlice) Exchange(ctx context.Context, subject string) (string, error) {
	if subject == "z-alice" {
		p.denied.Add(1)
		return "", identity.ErrZitadelIdentity
	}
	return (runtimeProvider{}).Exchange(ctx, subject)
}

func enableRuntimeIdentity(t *testing.T, f *fixture) identity.Links {
	t.Helper()
	links := identity.Links{DB: f.db, Issuer: "https://identity.invalid", BotID: 123}
	require.NoError(t, links.Bind(t.Context(), "alice", 101, "z-alice"))
	require.NoError(t, links.Bind(t.Context(), "bob", 202, "z-bob"))
	server := httptest.NewServer(
		api.AuthenticatedHandler(appservices.NewServices(f.db, appservices.Options{
			Delivery: f.b.Delivery,
		}), f.b.Host.Signer,
			slog.New(slog.DiscardHandler), api.ZitadelOwner(runtimeProvider{}, links)),
	)
	t.Cleanup(server.Close)
	f.b.API.Base, f.b.API.Links, f.b.API.Exchange = server.URL, links, runtimeProvider{}
	f.b.Host.Base = f.b.API.Base
	f.b.Logger = slog.New(slog.DiscardHandler)
	return links
}

func TestZitadelProviderDenialDoesNotStopMassageRefresh(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	handleVisible(t, f.b, message(100, 101, "/massage"))
	handleVisible(t, f.b, message(101, 202, "/massage"))
	enableRuntimeIdentity(t, f)
	provider := &deniedAlice{}
	f.b.API.Exchange = provider
	_, err := f.db.Exec(t.Context(), `UPDATE bot.massage_views SET view_hash='refresh'`)
	require.NoError(t, err)
	stop := runReconciliation(t, f.b)
	require.Eventually(t, func() bool {
		var hash string
		queryErr := f.db.QueryRow(t.Context(), `SELECT view_hash FROM bot.massage_views WHERE owner='bob'`).Scan(&hash)
		return queryErr == nil && hash != "refresh" && provider.denied.Load() > 0
	}, 5*time.Second, 20*time.Millisecond)
	stop()
	var hash string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT view_hash FROM bot.massage_views WHERE owner='alice'`).Scan(&hash),
	)
	assert.Equal(t, "refresh", hash)
}

func TestZitadelReminderRetriesUnattemptedIdentityFailure(t *testing.T) {
	t.Parallel()
	for _, failProvider := range []bool{false, true} {
		t.Run(map[bool]string{false: "link outage", true: "provider denial"}[failProvider], func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			f.b.Delivery.Fallback = 10 * time.Millisecond
			order, _ := cashOrder(t, f, "identity-reminder")
			require.NoError(t, f.b.DeliverNotifications(t.Context()))
			require.NoError(t, sandbox.ApplyOrderFixture(t.Context(), f.db,
				sandbox.OrderFixture{OrderID: order.ID, Age: 72 * time.Hour}))
			count, err := (orders.Service{DB: f.db, Delivery: f.b.Delivery}).QueueDueReminders(
				t.Context(),
				orders.DefaultReminderAfter,
			)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			before := chatMessages(t, f, 101)
			links := enableRuntimeIdentity(t, f)
			unavailable := &unavailableIdentity{links: links}
			unavailable.failed.Store(!failProvider)
			f.b.API.Links = unavailable
			if failProvider {
				f.b.API.Exchange = &deniedAlice{}
			}
			require.NoError(t, f.b.DeliverNotifications(t.Context()))
			var sent, delivered bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT delivery_state='sent' AND telegram_message_id>0,delivered_at IS NOT NULL
FROM core.order_notifications WHERE payload->>'kind'='reminder'`).
					Scan(&sent, &delivered),
			)
			assert.False(t, sent)
			assert.False(t, delivered)
			assert.Equal(t, before, chatMessages(t, f, 101), "identity failure must not reach Telegram")
			var deadline time.Time
			var generation int64
			var reason string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT greatest(n.available_at,q.not_before),n.delivery_attempt,n.failure
FROM core.order_notifications n JOIN core.delivery_queue q ON q.bot_id=n.bot_id
AND q.owner_kind='orders' AND q.owner_key=n.id::text AND q.effect_key='send'
WHERE n.payload->>'kind'='reminder' AND n.bot_id=$1 AND n.delivery_state='pending'`, f.b.Delivery.BotID).
					Scan(&deadline, &generation, &reason),
			)
			require.Positive(t, generation, "the unavailable identity must have been checked")
			require.Equal(t, "notification_preflight_unavailable", reason)
			unavailable.failed.Store(false)
			f.b.API.Exchange = runtimeProvider{}
			timer := time.NewTimer(time.Until(deadline))
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			}
			require.NoError(t, f.b.DeliverNotifications(t.Context()))
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT delivery_state='sent' AND telegram_message_id>0,delivered_at IS NOT NULL
FROM core.order_notifications WHERE payload->>'kind'='reminder'`).
					Scan(&sent, &delivered),
			)
			assert.True(t, sent)
			assert.True(t, delivered)
			messages := chatMessages(t, f, 101)
			require.Greater(t, len(messages), len(before))
			require.NoError(t, f.b.DeliverNotifications(t.Context()))
			assert.Equal(t, messages, chatMessages(t, f, 101))
			var receipts int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.notification_deliveries d
JOIN core.order_notifications n ON n.id=d.id WHERE n.payload->>'kind'='reminder'`).Scan(&receipts))
			assert.Equal(t, 1, receipts)
		})
	}
}
