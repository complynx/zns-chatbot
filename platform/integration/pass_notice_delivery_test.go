package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func drainPassNotices(t *testing.T, f *fixture) {
	t.Helper()
	for range 10 {
		notices, err := f.b.Host.PendingPassNotifications(t.Context())
		require.NoError(t, err)
		if len(notices) == 0 {
			return
		}
		require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
	}
	t.Fatal("pass notification queue did not drain")
}

func TestPassNoticeDeliveryLocaleHistoryAndRetry(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	f.b.API.HTTP = &http.Client{Transport: &failAcknowledgment{}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
	before := chatMessages(t, f, 101)
	require.NotEmpty(t, before)
	assert.Contains(t, before[len(before)-1].Text, "Танцы")
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.pass_notifications SET available_at=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
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
	f := registrationPaymentFixture(t)
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
	_, err := (passbooking.Service{DB: f.db}).Execute(
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
	WHERE recipient='alice' AND failure='telegram_forbidden'`).Scan(&blocked))
	assert.Positive(t, blocked)
}

func TestPassNoticeRefreshesExistingCard(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	handle(t, f.b, message(1000, 101, "/passes"))
	handle(t, f.b, passMenuClick(t, f, 101, 1001, "Dance"))
	before := passMenuCard(t, f, 101)
	booking, err := f.b.API.PassBooking(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = (passbooking.Service{DB: f.db}).Execute(
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
	require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
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

func TestPassNoticeStaleRetryKeepsOriginalHistory(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	f.b.API.HTTP = &http.Client{Transport: &failAcknowledgment{}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
	before := chatMessages(t, f, 101)
	require.Len(t, before, 1)
	booking, err := f.b.API.PassBooking(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = (passbooking.Service{DB: f.db}).Execute(
		t.Context(),
		"alice",
		bookingCommand("cancel", "stale-delivery", booking),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice';
	UPDATE core.pass_notifications SET available_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	drainPassNotices(t, f)
	var text string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT text FROM core.conversation_events
	WHERE owner='alice' AND source_key=(SELECT 'pass-notification-'||min(notice_id)::text FROM bot.pass_notification_deliveries)`).Scan(&text))
	assert.Equal(t, before[0].Text, text, "history retains the exact old locale after the notice became stale")
	assert.Equal(t, before[0], chatMessages(t, f, 101)[0], "known stale delivery is not resent or rewritten")
}
