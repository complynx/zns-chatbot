package integration_test

import (
	"net/http"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestMassageNotificationSplitRoleReplayAndRevocation(t *testing.T) {
	t.Parallel()
	f := restrictMemoryBotRole(t, massageBotFixture(t))
	service := massage.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	booking, err := service.Execute(t.Context(), "alice", massageBook("split-notice", "bob", 4, 1))
	require.NoError(t, err)
	_, err = f.b.DB.Exec(t.Context(), `SELECT id FROM core.massage_notices`)
	require.Error(t, err)
	_, err = f.b.DB.Exec(t.Context(), `SELECT owner FROM core.massage_notification_attempts`)
	require.Error(t, err)
	links := identity.Links{DB: f.db, Issuer: "https://massage.invalid", BotID: 123}
	require.NoError(t, links.Bind(t.Context(), "bob", 202, "z-bob"))
	_, err = f.db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=false WHERE owner='bob'`)
	require.NoError(t, err)
	f.b.API.Links, f.b.API.Exchange = links, runtimeProvider{}
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Empty(t, chatMessages(t, f, 202))
	// A missing identity binding is a preflight delay; no Telegram dispatch occurred.
	var state string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT delivery_state FROM core.massage_notices WHERE booking_id=$1 AND owner='bob' ORDER BY id LIMIT 1`, booking.ID).
			Scan(&state),
	)
	assert.Equal(t, "pending", state)
	_, err = f.db.Exec(t.Context(), `UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'
 WHERE bot_id=909090 AND owner_kind='massage' AND owner_key IN
 (SELECT id::text FROM core.massage_notices WHERE booking_id=$1)`, booking.ID)
	require.NoError(t, err)
	f.b.API.Links, f.b.API.Exchange = nil, nil
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.massage_notices SET available_at=clock_timestamp()-interval '1 second' WHERE booking_id=$1`,
		booking.ID,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	initial := len(chatMessages(t, f, 202))
	require.Positive(t, initial)
	var id, attempt int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id,delivery_attempt FROM core.massage_notices WHERE booking_id=$1 AND owner='bob' ORDER BY id LIMIT 1`, booking.ID).
			Scan(&id, &attempt),
	)
	ack := massage.NotificationFollowup{ID: id, Attempt: attempt, Done: true}
	require.NoError(t, f.b.Host.CompleteMassageNoticeFollowup(t.Context(), "bob", ack))
	require.Error(t, f.b.Host.CompleteMassageNoticeFollowup(t.Context(), "alice", ack))
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 202), initial)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id) VALUES($1,'alice','prior_short',909090)`,
		booking.ID,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.massage_bookings SET starts_at=now()-interval '20 minutes',ends_at=now()-interval '5 minutes' WHERE id=$1`,
		booking.ID,
	)
	require.NoError(t, err)
	indexSyntheticNotificationRows(t, f.db, "massage")
	before := len(chatMessages(t, f, 101))
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 101), before+1)
}

func TestMassageNotificationServiceAuthenticationAndRotation(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	service := massage.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	booking, err := service.Execute(t.Context(), "alice", massageBook("rotation", "bob", 4, 1))
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name)
 SELECT 'notice-'||lpad(n::text,3,'0'),1000+n,'Recipient' FROM generate_series(1,105)n`)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id)
 SELECT $1,id,'additional',909090 FROM core.users WHERE id LIKE 'notice-%'`, booking.ID)
	require.NoError(t, err)
	indexSyntheticNotificationRows(t, f.db, "massage")
	recipients, err := f.b.Host.MassageNoticeRecipients(t.Context())
	require.NoError(t, err)
	require.Len(t, recipients, 100)
	for _, recipient := range recipients {
		notices, noticeErr := f.b.Host.MassageDeliveryNotices(t.Context(), recipient.Owner)
		require.NoError(t, noticeErr)
		require.NotEmpty(t, notices)
		assert.Equal(t, "Алиса", notices[0].Client)
		assert.Equal(t, "Bob", notices[0].Specialist)
	}
	next, err := f.b.Host.MassageNoticeRecipients(t.Context())
	require.NoError(t, err)
	require.Len(t, next, 6, "prepared recipients remain leased; untouched lanes progress")
	assert.Equal(t, "notice-100", next[0].Owner)
	for _, path := range []string{"/internal/massage-notifications", "/internal/massage-notifications/bob/pending",
		"/internal/massage-notifications/bob/complete"} {
		method := http.MethodPost
		if path == "/internal/massage-notifications" {
			method = http.MethodGet
		}
		request, requestErr := http.NewRequestWithContext(t.Context(), method, f.b.API.Base+path, nil)
		require.NoError(t, requestErr)
		request.Header.Set("Authorization", "Bearer "+f.b.Host.Signer.Token("alice"))
		response, requestErr := http.DefaultClient.Do(request)
		require.NoError(t, requestErr)
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
}
