package integration_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Drive available pass and bot lane heads without leasing records as a probe.
func drainPassNotices(t *testing.T, f *fixture) {
	t.Helper()
	for range 100 {
		pumpBotDeliveries(t, f.b)
		found := false
		for _, entry := range botDeliveryCandidates(t, f.b) {
			if entry.Reference.Owner == delivery.Passes {
				found = true
				break
			}
		}
		if !found {
			return
		}
		require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
	}
	t.Fatal("pass notification queue did not drain")
}

func handlePassVisible(t *testing.T, f *fixture, update telegram.Update) {
	t.Helper()
	handle(t, f.b, update)
	drainPassNotices(t, f)
}
func TestPassNoticeDeliveryLocaleHistoryAndRetry(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	fault := &failPassFollowupAcknowledgment{}
	f.b.API.HTTP = &http.Client{Transport: fault}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.DeliverPassNotifications(t.Context()))
	require.True(t, fault.failed.Load())
	before := chatMessages(t, f, 101)
	require.NotEmpty(t, before)
	assert.Contains(t, before[len(before)-1].Text, "Танцы")
	var persisted int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_notification_deliveries d JOIN core.conversation_events e ON e.owner='alice' AND e.source_key='pass-notification-'||d.notice_id::text`).
			Scan(&persisted),
	)
	require.Equal(
		t,
		len(before),
		persisted,
		"the acknowledged wire outcome and private history precede the failed follow-up",
	)
	recoverPassNoticeFollowup(t, f)
	assert.Equal(t, before, chatMessages(t, f, 101), "lost completion must reuse the persisted delivery")
	drainPassNotices(t, f)
	after := chatMessages(t, f, 101)
	drainPassNotices(t, f)
	assert.Equal(t, after, chatMessages(t, f, 101))
	var archived int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events
	WHERE owner='alice' AND kind='system' AND source_key LIKE 'pass-notification-%'`).Scan(&archived))
	assert.Equal(t, len(after), archived, "each delivered notice appears once in the private agent history")
}
func TestPassNoticeServiceIdentityAndBlockedRecipient(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	for _, test := range []struct {
		token  string
		status int
	}{
		{f.b.Host.Signer.Token("bob"), http.StatusUnauthorized},
		{f.b.Host.Signer.DeliveryToken(), http.StatusOK},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			f.b.API.Base+"/internal/pass-notifications", http.NoBody)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+test.token)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		assert.Equal(t, test.status, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
	service := passbooking.Service{DB: f.db, Delivery: f.b.Delivery}
	aliceCommand := bookingCommand("solo", "registration-payment", passbooking.Booking{})
	aliceCommand.PaymentAdmin = "bob"
	alice, err := service.Execute(t.Context(), "alice", aliceCommand)
	require.NoError(t, err)
	require.Equal(t, "assigned", alice.State)
	_, err = service.Execute(
		t.Context(),
		"bob",
		bookingCommand("solo", "bob-register", passbooking.Booking{}),
	)
	require.NoError(t, err)
	post(t, f.fake.URL+"/lab/blocked", map[string]any{"user": 101, "blocked": true})
	drainPassNotices(t, f)
	assert.Empty(t, chatMessages(t, f, 101))
	assert.NotEmpty(t, chatMessages(t, f, 202), "one blocked recipient must not prevent other deliveries")
	var blocked int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_notifications
	WHERE recipient='alice' AND delivery_state='failed' AND failure='telegram_recipient_rejected'`).Scan(&blocked))
	assert.Positive(t, blocked)
}

func TestPassNoticeRefreshesExistingCard(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	handlePassVisible(t, f, message(1000, 101, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 1001, "Dance"))
	before := passMenuCard(t, f, 101)
	booking, err := f.b.API.PassBooking(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = (passbooking.Service{DB: f.db, Delivery: f.b.Delivery}).Execute(
		t.Context(),
		"alice",
		bookingCommand("cancel", "cancel-notice", booking),
	)
	require.NoError(t, err)
	drainPassNotices(t, f)
	after := passMenuCard(t, f, 101)
	assert.Equal(t, before.ID, after.ID, "delivery updates the existing manual GUI")
	assert.Contains(t, after.Text, "Cancelled")
	assert.NotContains(t, after.Text, "Total to pay")
}

func TestPassNoticeDeliveryHistoryIsAtomic(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`CREATE FUNCTION core.fail_pass_notice_history() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN IF NEW.source_key LIKE 'pass-notification-%' THEN RAISE EXCEPTION 'synthetic history failure'; END IF;
	RETURN NEW; END $$;
	CREATE TRIGGER fail_pass_notice_history BEFORE INSERT ON core.conversation_events
	FOR EACH ROW EXECUTE FUNCTION core.fail_pass_notice_history()`,
	)
	require.NoError(t, err)
	require.Error(t, f.b.DeliverPassNotifications(t.Context()), "the injected history failure is reported")
	var receipts int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_notification_deliveries`).Scan(&receipts),
	)
	assert.Zero(t, receipts, "a history failure must roll back the delivery receipt too")
	require.Len(
		t,
		chatMessages(t, f, 101),
		1,
		"Telegram already accepted the message; this is the documented pre-persistence window",
	)
}

func recoverPassNoticeFollowup(t *testing.T, f *fixture) {
	t.Helper()
	var leaseUntil time.Time
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT lease_until FROM core.pass_notifications WHERE id=(SELECT min(notice_id) FROM bot.pass_notification_deliveries)`).
			Scan(&leaseUntil),
	)
	timer := time.NewTimer(time.Until(leaseUntil))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	require.NoError(t, f.b.RecoverPassNotifications(t.Context()))
}

type failPassFollowupAcknowledgment struct{ failed atomic.Bool }

func (transport *failPassFollowupAcknowledgment) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/internal/pass-notifications/followup" && transport.failed.CompareAndSwap(false, true) {
		return nil, errors.New("simulated lost pass follow-up acknowledgment")
	}
	return http.DefaultTransport.RoundTrip(request)
}
func TestPassNoticeStaleRetryKeepsOriginalHistory(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	fault := &failPassFollowupAcknowledgment{}
	f.b.API.HTTP = &http.Client{Transport: fault}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.DeliverPassNotifications(t.Context()))
	require.True(t, fault.failed.Load(), "failure occurs after the canonical send and private history are persisted")
	before := chatMessages(t, f, 101)
	require.Len(t, before, 1)
	var storedMessageID int64
	var storedText string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT d.message_id,e.text FROM bot.pass_notification_deliveries d JOIN core.conversation_events e ON e.owner='alice' AND e.source_key='pass-notification-'||d.notice_id::text ORDER BY d.notice_id LIMIT 1`).
			Scan(&storedMessageID, &storedText),
	)
	assert.Equal(t, before[0].ID, storedMessageID)
	assert.Equal(t, before[0].Text, storedText)
	booking, err := f.b.API.PassBooking(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = (passbooking.Service{DB: f.db, Delivery: f.b.Delivery}).Execute(
		t.Context(),
		"alice",
		bookingCommand("cancel", "stale-delivery", booking),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	recoverPassNoticeFollowup(t, f)
	var text string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT text FROM core.conversation_events
	WHERE owner='alice' AND source_key=(SELECT 'pass-notification-'||min(notice_id)::text FROM bot.pass_notification_deliveries)`).Scan(&text))
	assert.Equal(t, before[0].Text, text, "history retains the exact old locale after the notice became stale")
	after := chatMessages(t, f, 101)
	require.NotEmpty(t, after)
	assert.Equal(t, before[0], after[0], "known stale delivery is not resent or rewritten")
	var followupPending bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT followup_pending FROM core.pass_notifications WHERE id=(SELECT min(notice_id) FROM bot.pass_notification_deliveries)`).
			Scan(&followupPending),
	)
	assert.False(t, followupPending, "the stale known delivery completes its follow-up")
}
