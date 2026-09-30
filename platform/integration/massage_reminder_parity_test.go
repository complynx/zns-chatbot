package integration_test

import (
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestMassageReminderSourceWindow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		offset    time.Duration
		cancelled bool
		want      int64
	}{
		{"before party tolerance", -2*time.Hour - time.Microsecond, false, 0},
		{"at party tolerance", -2 * time.Hour, false, 3},
		{"already started", -time.Minute, false, 3},
		{"at start", 0, false, 3},
		{"next exact upper bound", 5 * time.Minute, false, 2},
		{"short exact upper bound", 10 * time.Minute, false, 1},
		{"long exact upper bound", time.Hour, false, 0},
		{"cancelled", 0, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db, service, start := massageFixture(t)
			booking, err := service.Execute(t.Context(), "alice", massageBook("source-window", "bob", 1, 1))
			require.NoError(t, err)
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.massage_bookings SET starts_at=$2,ends_at=$2::timestamptz+interval '15 minutes',cancelled_at=CASE WHEN $3 THEN now() ELSE NULL END WHERE id=$1`,
				booking.ID,
				start.Add(test.offset),
				test.cancelled,
			)
			require.NoError(t, err)
			service.Now = func() time.Time { return start }
			count, err := service.QueueReminders(t.Context(), booking.Event)
			require.NoError(t, err)
			assert.Equal(t, test.want, count)
			count, err = service.QueueReminders(t.Context(), booking.Event)
			require.NoError(t, err)
			assert.Zero(t, count)
		})
	}
}

func TestMassageAdditionalNoticeSourceWindow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		offset    time.Duration
		cancelled bool
		want      int
	}{
		{"before", -time.Minute, false, 1},
		{"at", 0, false, 1},
		{"after", time.Microsecond, false, 0},
		{"cancelled", -time.Minute, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := massageBotFixture(t)
			service := massage.Service{DB: f.db, Delivery: f.b.Delivery}
			_, err := service.SetPreferences(t.Context(), "bob", "sandbox-festival", massage.Preferences{})
			require.NoError(t, err)
			booking, err := service.Execute(t.Context(), "alice", massageBook("additional-window", "bob", 1, 1))
			require.NoError(t, err)
			now := booking.Start.Add(test.offset)
			service.Now = func() time.Time { return now }
			services := notificationFixtureServices(f.db, appservices.Options{})
			services.Massage = service
			services.DerivedMutations.Massage = service
			server := httptest.NewServer(api.Handler(services, f.b.Host.Signer, slog.New(slog.DiscardHandler)))
			t.Cleanup(server.Close)
			f.b.API.Base, f.b.Host.Base = server.URL, server.URL
			id := seedMassageFixtureNotice(t, f, booking.ID, "alice", "additional")
			// Historical handled markers are terminal; they are not pending queue work.
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.massage_notices(booking_id,owner,kind,sent_at,delivery_state)
 VALUES($1,'alice','prior_long',now(),'sent'),($1,'alice','prior_short',now(),'sent'),($1,'bob','next',now(),'sent')`,
				booking.ID,
			)
			require.NoError(t, err)
			if test.cancelled {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE core.massage_bookings SET cancelled_at=now() WHERE id=$1`,
					booking.ID,
				)
				require.NoError(t, err)
			}
			notices, err := service.PendingNotices(t.Context(), "alice")
			require.NoError(t, err)
			assert.Len(t, notices, test.want)
			foreign, err := service.PendingNotices(t.Context(), "visitor")
			require.NoError(t, err)
			assert.Empty(t, foreign)
			foreignDelivery, err := service.DeliveryNotices(t.Context(), "visitor")
			require.NoError(t, err)
			assert.Empty(t, foreignDelivery)
			// Recipient discovery includes work that the adapter must cancel as stale.
			recipients, err := service.NoticeRecipients(t.Context())
			require.NoError(t, err)
			require.Len(t, recipients, 1)
			require.Equal(t, "alice", recipients[0].Owner)
			state := "cancelled"
			if test.want == 1 {
				state = "sent"
			}
			deliverMassageFixtureNotice(t, f, id, state)
			assert.Len(t, chatMessages(t, f, 101), test.want)
			assert.Empty(t, chatMessages(t, f, 202))
			var messageID int64
			require.NoError(t, f.db.QueryRow(t.Context(),
				`SELECT telegram_message_id FROM core.massage_notices WHERE id=$1`, id).Scan(&messageID))
			if test.want == 1 {
				assert.Positive(t, messageID)
			} else {
				assert.Zero(t, messageID, "noncurrent work must retire without a wire send")
			}
			deliverMassageFixtureNotice(t, f, id, state)
			assert.Len(t, chatMessages(t, f, 101), test.want)
			recipients, err = service.NoticeRecipients(t.Context())
			require.NoError(t, err)
			assert.Empty(t, recipients)
		})
	}
}
