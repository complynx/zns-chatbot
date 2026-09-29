package integration_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func syntheticDeliverySettings() delivery.Settings {
	return delivery.Settings{
		BotID:        909090,
		BotInterval:  time.Millisecond,
		ChatInterval: time.Millisecond,
		Fallback:     30 * time.Second,
	}
}

// This test binding uses real API/Host authorization and explicit synthetic identity.
func configureDeliveryFixture(t *testing.T, f *fixture) adminmessage.Service {
	t.Helper()
	services := notificationFixtureServices(f.db, appservices.Options{})
	services.AdminMessages.Delivery = syntheticDeliverySettings()
	services.Registration.Delivery = syntheticDeliverySettings()
	services.DerivedMutations.Registration = services.Registration
	server := httptest.NewServer(api.Handler(services, f.b.Host.Signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	f.b.API.Base = server.URL
	f.b.Host.Base = server.URL
	return services.AdminMessages
}

func enqueueSyntheticDelivery(t *testing.T, s adminmessage.Service, key, chat string) {
	t.Helper()
	preview, err := s.PreviewCommand(t.Context(), "bob", key, `/send_message_to `+chat+` --msg "synthetic"`)
	require.NoError(t, err)
	require.NoError(t, s.Enqueue(t.Context(), "bob", preview.ID))
}

func TestDurableDeliveryIdentityIsBoundAtEnqueue(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	unbound := adminmessage.Service{DB: db}
	enqueueSyntheticDelivery(t, unbound, "unbound", "101")
	_, _, err := unbound.Claim(t.Context())
	require.ErrorIs(t, err, delivery.ErrSettings)
	bound := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	_, found, err := bound.Claim(t.Context())
	require.NoError(t, err)
	require.False(t, found)
	enqueueSyntheticDelivery(t, bound, "bound", "202")
	other := bound
	other.Delivery.BotID++
	_, found, err = other.Claim(t.Context())
	require.NoError(t, err)
	require.False(t, found)
	item, found, err := bound.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "202", item.Destination.Chat)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_message_deliveries WHERE bot_id IS NULL`).
			Scan(&count),
	)
	assert.Equal(t, 1, count)
}

func TestDurableDeliveryPreparedAttemptFencingAndUnknownLane(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	enqueueSyntheticDelivery(t, s, "first", "101")
	enqueueSyntheticDelivery(t, s, "second", "101")
	first, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`,
		first.ID,
	)
	require.NoError(t, err)
	retry, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first.ID, retry.ID)
	require.Greater(t, retry.Attempt, first.Attempt)
	_, err = s.BeginDelivery(t.Context(), delivery.Attempt{ID: first.ID, Generation: first.Attempt})
	require.Error(t, err)
	gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: retry.ID, Generation: retry.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	err = s.CompleteDelivery(
		t.Context(),
		adminmessage.Completion{
			ID:      first.ID,
			Attempt: first.Attempt,
			Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 91},
		},
	)
	require.Error(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`,
		first.ID,
	)
	require.NoError(t, err)
	restarted := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	_, found, err = restarted.Claim(t.Context())
	require.NoError(t, err)
	require.False(t, found, "an ambiguous lane head must block its successor")
	enqueueSyntheticDelivery(t, s, "independent", "202")
	independent, found, err := restarted.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "202", independent.Destination.Chat)
	var state string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM core.admin_message_deliveries WHERE id=$1`, first.ID).Scan(&state),
	)
	assert.Equal(t, "unknown", state)
}

func TestDurableDeliveryLockedLaneDoesNotBlockIndependentChat(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	enqueueSyntheticDelivery(t, s, "locked", "101")
	enqueueSyntheticDelivery(t, s, "behind", "101")
	enqueueSyntheticDelivery(t, s, "free", "202")
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()
	var id int64
	require.NoError(
		t,
		tx.QueryRow(t.Context(), `SELECT id FROM core.admin_message_deliveries ORDER BY id LIMIT 1 FOR UPDATE`).
			Scan(&id),
	)
	item, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "202", item.Destination.Chat)
}

func TestDurableDeliveryReauthorizesAfterCooldown(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	s := configureDeliveryFixture(t, f)
	enqueueSyntheticDelivery(t, s, "withdraw", "101")
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":120}}`))
	}))
	t.Cleanup(server.Close)
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	_, err := f.db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins WHERE owner='bob'; UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	assert.EqualValues(t, 1, sends.Load())
	var state string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state FROM core.admin_message_deliveries`).Scan(&state))
	assert.Equal(t, "cancelled", state)
}

func TestDurableDeliveryRecipientFailureDoesNotStopNextItem(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	s := configureDeliveryFixture(t, f)
	enqueueSyntheticDelivery(t, s, "blocked", "101")
	enqueueSyntheticDelivery(t, s, "allowed", "202")
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if sends.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":403}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":52}}`))
	}))
	t.Cleanup(server.Close)
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	_, err := f.db.Exec(t.Context(), `UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	assert.EqualValues(t, 2, sends.Load())
	var states []string
	rows, err := f.db.Query(t.Context(), `SELECT state FROM core.admin_message_deliveries ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var state string
		require.NoError(t, rows.Scan(&state))
		states = append(states, state)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"failed", "sent"}, states)
}
