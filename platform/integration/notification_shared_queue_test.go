package integration_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func notificationQueueOwner(domain string) delivery.Owner {
	switch domain {
	case "orders":
		return delivery.Orders
	case "registration":
		return delivery.Passes
	case "massage":
		return delivery.Massage
	case "food":
		return delivery.Food
	default:
		panic("unknown synthetic notification domain")
	}
}

// Only explicit test-seeded rows have a zero captured chat. Production enqueue
// must already index its own rows atomically; this helper does not repair them.
func indexSyntheticNotificationRows(t *testing.T, db *pgxpool.Pool, domain string) {
	t.Helper()
	table, recipient := "", ""
	switch domain {
	case "orders":
		table, recipient = "core.order_notifications", "recipient"
	case "registration":
		table, recipient = "core.pass_notifications", "recipient"
	case "massage":
		table, recipient = "core.massage_notices", "owner"
	case "food":
		table, recipient = "core.food_notifications", "owner"
	default:
		t.Fatal("unknown synthetic notification domain")
	}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	rows, err := tx.Query(
		t.Context(),
		"UPDATE "+table+" n SET delivery_chat=u.telegram_id FROM core.users u WHERE u.id=n."+recipient+" AND n.delivery_chat=0 AND n.bot_id=$1 RETURNING n.id,n.delivery_chat",
		syntheticDeliverySettings().BotID,
	)
	require.NoError(t, err)
	inserted, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (delivery.Registration, error) {
		var id, chat int64
		scanErr := row.Scan(&id, &chat)
		return delivery.Registration{
			Reference: delivery.Reference{
				Owner:  notificationQueueOwner(domain),
				Key:    strconv.FormatInt(id, 10),
				Effect: "send",
			},
			Destination: delivery.Destination{Chat: strconv.FormatInt(chat, 10)},
			Class:       delivery.Background,
		}, scanErr
	})
	require.NoError(t, err)
	slices.SortStableFunc(inserted, func(a, b delivery.Registration) int {
		first, _ := strconv.ParseInt(a.Reference.Key, 10, 64)
		second, _ := strconv.ParseInt(b.Reference.Key, 10, 64)
		if first < second {
			return -1
		}
		if first > second {
			return 1
		}
		return 0
	})
	require.NoError(t, delivery.RegisterBatch(t.Context(), tx, syntheticDeliverySettings().BotID, inserted))
	require.NoError(t, tx.Commit(t.Context()))
}

func exactNotificationDelivery(r *notificationRuntimeFixture, domain string) func(context.Context, int64) error {
	switch domain {
	case "orders":
		return r.f.b.DeliverOrderNotification
	case "registration":
		return r.f.b.DeliverPassNotification
	case "massage":
		return r.f.b.DeliverMassageNotification
	case "food":
		return r.f.b.DeliverFoodNotification
	default:
		panic("unknown synthetic notification domain")
	}
}

func recoverNotificationDelivery(r *notificationRuntimeFixture, domain string) func(context.Context) error {
	switch domain {
	case "orders":
		return r.f.b.RecoverOrderNotifications
	case "registration":
		return r.f.b.RecoverPassNotifications
	case "massage":
		return r.f.b.RecoverMassageNotifications
	case "food":
		return r.f.b.RecoverFoodNotifications
	default:
		panic("unknown synthetic notification domain")
	}
}

// Cancel after a fully received Telegram response, before the adapter persists
// it. This tests a known response crossing the worker cancellation boundary.
type notificationCancelTransport struct {
	base   http.RoundTripper
	cancel context.CancelFunc
	once   sync.Once
}

func (c *notificationCancelTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(request)
	if err != nil || !strings.HasSuffix(request.URL.Path, "/sendMessage") {
		return response, err
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	c.once.Do(c.cancel)
	return response, nil
}

func TestNotificationSharedQueueKnownResultSurvivesCancellation(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r.f.b.TG.HTTP = &http.Client{
				Transport: &notificationCancelTransport{base: http.DefaultTransport, cancel: cancel},
			}
			// Followup observes cancellation; primary completion has an independent bound.
			_ = exactNotificationDelivery(r, domain)(ctx, r.first)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			before := r.status(t, r.first)
			require.Equal(t, "sent", before.State)
			require.Positive(t, before.MessageID)
			require.Equal(t, 1, spy.calls(202))
			_, err := r.f.db.Exec(
				t.Context(),
				"UPDATE "+r.table+" SET lease_until=NULL,available_at=clock_timestamp() WHERE id=$1",
				r.first,
			)
			require.NoError(t, err)
			r.wake(t)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			assert.Equal(t, before.MessageID, r.status(t, r.first).MessageID)
			assert.Equal(t, 1, spy.calls(202), "recovery must never resend a known primary")
		})
	}
}

func TestNotificationSharedQueueExactSelectionAndRecovery(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "")
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.second))
			assert.Zero(t, spy.calls(202), "a requested non-head must not claim the first row")
			assert.Zero(t, r.status(t, r.first).Attempt)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			assert.Zero(t, spy.calls(202), "recovery must not pick a fresh primary")
			require.NoError(t, dispatch(t.Context(), r.other))
			assert.Positive(t, spy.calls(101), "an independent chat progresses")
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, "sent", r.status(t, r.first).State)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 1, spy.calls(202), "stale shared candidate is harmless")
		})
	}
}

func TestNotificationSharedQueueOrdersBeforePasses(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, _ := cashOrder(t, f, "shared-order-first")
	var orderNotice int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT id FROM core.order_notifications WHERE order_id=$1 AND recipient='bob'", order.ID).
			Scan(&orderNotice),
	)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('dance',now()+interval '30 days');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','bob');
 INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader'),('bob','follower')`,
	)
	require.NoError(t, err)
	service := passbooking.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	for _, owner := range []string{"bob", "alice"} {
		_, err = service.Execute(t.Context(), owner, bookingCommand("solo", "shared-"+owner, passbooking.Booking{}))
		require.NoError(t, err)
	}
	var passNotice, otherNotice int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT min(id) FROM core.pass_notifications WHERE recipient='bob'").
			Scan(&passNotice),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT min(id) FROM core.pass_notifications WHERE recipient='alice'").
			Scan(&otherNotice),
	)
	spy := attachNotificationWireSpy(t, f, "")
	require.NoError(t, f.b.DeliverPassNotification(t.Context(), passNotice))
	assert.Zero(t, spy.calls(202), "a different owner cannot overtake the order notice")
	require.NoError(t, f.b.DeliverPassNotification(t.Context(), otherNotice))
	assert.Positive(t, spy.calls(101), "the independent chat is not blocked")
	clock := &notificationRuntimeFixture{f: f, table: "core.order_notifications"}
	clock.wake(t)
	require.NoError(t, f.b.DeliverOrderNotification(t.Context(), orderNotice))
	assert.Equal(t, 1, spy.calls(202))
	clock.wake(t)
	require.NoError(t, f.b.DeliverPassNotification(t.Context(), passNotice))
	var state string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT delivery_state FROM core.pass_notifications WHERE id=$1", passNotice).
			Scan(&state),
	)
	assert.Equal(t, "sent", state, "the next domain proceeds after canonical completion")
}

func TestNotificationSharedQueueMissingIndexIsNotAdopted(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "")
			_, err := r.f.db.Exec(
				t.Context(),
				"DELETE FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send'",
				syntheticDeliverySettings().BotID,
				string(notificationQueueOwner(domain)),
				strconv.FormatInt(r.first, 10),
			)
			require.NoError(t, err)
			require.Error(
				t,
				exactNotificationDelivery(r, domain)(t.Context(), r.first),
				"exact selection must not silently hide index corruption",
			)
			require.Error(
				t,
				recoverNotificationDelivery(r, domain)(t.Context()),
				"corrupt owner/index binding must be observable",
			)
			assert.Zero(t, spy.calls(202))
			var exists bool
			require.NoError(
				t,
				r.f.db.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send')", syntheticDeliverySettings().BotID, string(notificationQueueOwner(domain)), strconv.FormatInt(r.first, 10)).
					Scan(&exists),
			)
			assert.False(t, exists, "recovery never adopts or backfills an unindexed intent")
		})
	}
}
