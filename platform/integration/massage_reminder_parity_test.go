package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	db, service, start := massageFixture(t)
	booking, err := service.Execute(t.Context(), "alice", massageBook("additional-window", "bob", 1, 1))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.massage_notices(booking_id,owner,kind) VALUES($1,'alice','additional')`,
		booking.ID,
	)
	require.NoError(t, err)
	// Suppress ordinary reminders as imported marker presence does.
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.massage_notices(booking_id,owner,kind,sent_at) VALUES($1,'alice','prior_long',now()),($1,'alice','prior_short',now()),($1,'bob','next',now())`,
		booking.ID,
	)
	require.NoError(t, err)
	for _, test := range []struct {
		name string
		now  time.Time
		want int
	}{
		{"before", start, 1}, {"at", booking.Start, 1}, {"after", booking.Start.Add(time.Microsecond), 0},
	} {
		service.Now = func() time.Time { return test.now }
		notices, noticeErr := service.PendingNotices(t.Context(), "alice")
		require.NoError(t, noticeErr)
		assert.Len(t, notices, test.want, test.name)
		delivery, deliveryErr := service.DeliveryNotices(t.Context(), "alice")
		require.NoError(t, deliveryErr)
		assert.Len(t, delivery, test.want, test.name)
		recipients, recipientErr := service.NoticeRecipients(t.Context())
		require.NoError(t, recipientErr)
		owners := []string{}
		for _, recipient := range recipients {
			owners = append(owners, recipient.Owner)
		}
		if test.want == 1 {
			assert.Contains(t, owners, "alice")
		} else {
			assert.NotContains(t, owners, "alice")
		}
	}
	notices, err := service.PendingNotices(t.Context(), "visitor")
	require.NoError(t, err)
	assert.Empty(t, notices)
	service.Now = func() time.Time { return start }
	_, err = db.Exec(t.Context(), `UPDATE core.massage_bookings SET cancelled_at=now() WHERE id=$1`, booking.ID)
	require.NoError(t, err)
	notices, err = service.PendingNotices(t.Context(), "alice")
	require.NoError(t, err)
	assert.Empty(t, notices)
	delivery, err := service.DeliveryNotices(t.Context(), "alice")
	require.NoError(t, err)
	assert.Empty(t, delivery)
}
