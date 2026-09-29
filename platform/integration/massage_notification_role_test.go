package integration_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestMassageNotificationSplitRoleReplayAndRevocation(t *testing.T) {
	t.Parallel()
	f := restrictMemoryBotRole(t, massageBotFixture(t))
	service := massage.Service{DB: f.db}
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
	pending, err := service.PendingNotices(t.Context(), "bob")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	f.b.API.Links, f.b.API.Exchange = nil, nil
	base := f.b.TG.Base
	f.b.TG.Base = base + "/unavailable"
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	f.b.TG.Base = base
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	initial := len(chatMessages(t, f, 202))
	require.Positive(t, initial)
	_, err = f.db.Exec(t.Context(), `UPDATE core.massage_notices SET sent_at=NULL WHERE booking_id=$1`, booking.ID)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 202), initial)
	require.NoError(t, f.b.API.CompleteMassageNotice(t.Context(), "bob", pending[0].ID))
	require.Error(t, f.b.API.CompleteMassageNotice(t.Context(), "alice", pending[0].ID))
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_notices(booking_id,owner,kind) VALUES($1,'alice','prior_short')`,
		booking.ID,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.massage_bookings SET starts_at=now()-interval '20 minutes',ends_at=now()-interval '5 minutes' WHERE id=$1`,
		booking.ID,
	)
	require.NoError(t, err)
	before := len(chatMessages(t, f, 101))
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 101), before+1)
}

func TestMassageNotificationServiceAuthenticationAndRotation(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	service := massage.Service{DB: f.db}
	booking, err := service.Execute(t.Context(), "alice", massageBook("rotation", "bob", 4, 1))
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name)
 SELECT 'notice-'||lpad(n::text,3,'0'),1000+n,'Recipient' FROM generate_series(1,105)n`)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.massage_notices(booking_id,owner,kind)
 SELECT $1,id,'additional' FROM core.users WHERE id LIKE 'notice-%'`, booking.ID)
	require.NoError(t, err)
	recipients, err := f.b.API.MassageNoticeRecipients(t.Context())
	require.NoError(t, err)
	require.Len(t, recipients, 100)
	for _, recipient := range recipients {
		notices, noticeErr := f.b.API.MassageDeliveryNotices(t.Context(), recipient.Owner)
		require.NoError(t, noticeErr)
		require.NotEmpty(t, notices)
		assert.Equal(t, "Алиса", notices[0].Client)
		assert.Equal(t, "Bob", notices[0].Specialist)
	}
	next, err := f.b.API.MassageNoticeRecipients(t.Context())
	require.NoError(t, err)
	require.Len(t, next, 100)
	assert.Equal(t, "notice-100", next[0].Owner)
	for _, path := range []string{"/internal/massage-notifications", "/internal/massage-notifications/bob/pending",
		"/internal/massage-notifications/bob/" + strconv.FormatInt(1, 10) + "/complete"} {
		method := http.MethodPost
		if path == "/internal/massage-notifications" {
			method = http.MethodGet
		}
		request, requestErr := http.NewRequestWithContext(t.Context(), method, f.b.API.Base+path, nil)
		require.NoError(t, requestErr)
		request.Header.Set("Authorization", "Bearer "+f.b.API.Signer.Token("alice"))
		response, requestErr := http.DefaultClient.Do(request)
		require.NoError(t, requestErr)
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
}
