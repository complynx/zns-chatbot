package integration_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func massageFixture(t *testing.T) (*pgxpool.Pool, massage.Service, time.Time) {
	t.Helper()
	db := database(t)
	start := time.Date(2030, time.October, 2, 18, 0, 0, 0, time.UTC)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book,language) VALUES
	('master',404,'Master',true,'en'),('client',505,'Client',true,'en');
	INSERT INTO core.massage_events(id) VALUES('sandbox-festival');
	INSERT INTO core.massage_specialists(event_id,owner,name,legacy_table_flag) VALUES
	('sandbox-festival','bob','Bob',true),('sandbox-festival','master','Master',true)`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.massage_parties(id,event_id,starts_at,ends_at,tables)
	VALUES('night','sandbox-festival',$1,$2,1);
	`, start, start.Add(4*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at)
	VALUES('sandbox-festival','bob',$1,$2),('sandbox-festival','master',$1,$2)`, start, start.Add(4*time.Hour))
	require.NoError(t, err)
	return db, massage.Service{DB: db, Now: func() time.Time { return start.Add(-time.Hour) }}, start
}

func massageBook(key, specialist string, slot, length int) massage.Command {
	return massage.Command{
		Key:        key,
		Action:     "book",
		Event:      "sandbox-festival",
		Party:      "night",
		Specialist: specialist,
		Slot:       slot,
		Length:     length,
	}
}

func TestMassageReminderWindowIncludesStartedBookings(t *testing.T) {
	t.Parallel()
	db, service, start := massageFixture(t)
	booking, err := service.Execute(t.Context(), "alice", massageBook("window", "bob", 1, 1))
	require.NoError(t, err)
	service.Now = func() time.Time { return booking.Start.Add(-time.Hour) }
	count, err := service.QueueReminders(t.Context(), booking.Event)
	require.NoError(t, err)
	assert.Zero(t, count)
	service.Now = func() time.Time { return booking.Start }
	count, err = service.QueueReminders(t.Context(), booking.Event)
	require.NoError(t, err)
	assert.EqualValues(t, 3, count)
	_, err = db.Exec(t.Context(), `DELETE FROM core.massage_notices WHERE kind<>'booked'`)
	require.NoError(t, err)
	service.Now = func() time.Time { return start.Add(21 * time.Minute) }
	count, err = service.QueueReminders(t.Context(), booking.Event)
	require.NoError(t, err)
	assert.EqualValues(t, 3, count)
}

func massageCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, status, problem.Status)
	assert.Equal(t, code, problem.Code)
}

func TestMassageBookCancelReplayAndPrivacy(t *testing.T) {
	t.Parallel()
	db, service, start := massageFixture(t)
	command := massageBook("first", "bob", 1, 2)
	booking, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, 57, booking.Price)
	assert.Equal(t, 1600, booking.PriceRUB)
	assert.Equal(t, 35*time.Minute, booking.End.Sub(booking.Start))
	assert.WithinDuration(t, start.Add(20*time.Minute), booking.Start, 0)
	again, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, booking.ID, again.ID)
	assert.Equal(t, booking.Version, again.Version)
	assert.WithinDuration(t, booking.Start, again.Start, 0)
	command.Length = 3
	_, err = service.Execute(t.Context(), "alice", command)
	massageCode(t, err, http.StatusConflict, "idempotency_conflict")
	cancel := massage.Command{
		Key:     "cancel",
		Action:  "cancel",
		Event:   booking.Event,
		Booking: booking.ID,
		Version: booking.Version,
	}
	_, err = service.Execute(t.Context(), "visitor", cancel)
	massageCode(t, err, http.StatusNotFound, "not_found")
	_, err = service.Execute(t.Context(), "bob", cancel)
	massageCode(t, err, http.StatusNotFound, "not_found")
	cancel.Version++
	_, err = service.Execute(t.Context(), "alice", cancel)
	massageCode(t, err, http.StatusConflict, "stale_version")
	cancel.Version = booking.Version
	removed, err := service.Execute(t.Context(), "alice", cancel)
	require.NoError(t, err)
	require.NotNil(t, removed.CancelledAt)
	assert.Equal(t, int64(2), removed.Version)
	_, err = service.Execute(t.Context(), "visitor", massageBook("replacement", "master", 1, 2))
	require.NoError(t, err, "Python massage does not use generic can_book gate")
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_bookings`).Scan(&count))
	assert.Equal(t, 2, count)
	_, err = service.Bookings(t.Context(), "alice", booking.Event, "night", "timetable")
	massageCode(t, err, http.StatusForbidden, "forbidden")
	calendar, err := service.Bookings(t.Context(), "bob", booking.Event, "night", "timetable")
	require.NoError(t, err)
	require.Len(t, calendar, 1)
	assert.Equal(t, "visitor", calendar[0].Owner)
	clientList, err := service.Bookings(t.Context(), "bob", booking.Event, "night", "clients")
	require.NoError(t, err)
	assert.Empty(t, clientList)
}

func TestMassageSharedTableConcurrency(t *testing.T) {
	t.Parallel()
	_, service, _ := massageFixture(t)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for index, actor := range []string{"alice", "visitor"} {
		group.Go(func() {
			staff := []string{"bob", "master"}[index]
			_, err := service.Execute(t.Context(), actor, massageBook("same-slot", staff, 2, 1))
			results <- err
		})
	}
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			massageCode(t, err, http.StatusConflict, "unavailable")
		}
	}
	assert.Equal(t, 1, success)
}

func TestMassageServiceTimeAndDurationBoundaries(t *testing.T) {
	t.Parallel()
	db, service, start := massageFixture(t)
	service.Now = func() time.Time { return start.Add(5 * time.Minute) }
	_, err := service.Execute(t.Context(), "alice", massageBook("deadline", "bob", 1, 1))
	require.NoError(t, err)
	service.Now = func() time.Time { return start.Add(5*time.Minute + time.Nanosecond) }
	_, err = service.Execute(t.Context(), "visitor", massageBook("late", "master", 1, 1))
	massageCode(t, err, http.StatusConflict, "timeout")
	_, err = service.Execute(t.Context(), "visitor", massageBook("regular-four", "master", 5, 4))
	massageCode(t, err, http.StatusBadRequest, "invalid_length_or_party")
	_, err = db.Exec(t.Context(), `UPDATE core.massage_specialists SET max_length=1 WHERE owner='master'`)
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "visitor", massageBook("too-long", "master", 5, 2))
	massageCode(t, err, http.StatusConflict, "specialist_unavailable")
	service.Now = func() time.Time { return start.Add(65 * time.Minute) }
	instant, err := service.Execute(t.Context(), "master", massage.Command{
		Key: "instant-four", Action: "instant", Event: "sandbox-festival", Party: "night", Length: 4,
	})
	require.NoError(t, err, "Python instant buttons do not apply the regular specialist duration filter")
	assert.Equal(t, 3, instant.Slot)
	assert.Equal(t, 4, instant.Length)
	_, err = service.Slots(t.Context(), "alice", "foreign-event", "night", 1)
	massageCode(t, err, http.StatusNotFound, "not_found")
}

func TestMassageLimitsInstantAndNotifications(t *testing.T) {
	t.Parallel()
	db, service, start := massageFixture(t)
	_, err := service.Execute(t.Context(), "alice", massageBook("a", "bob", 2, 1))
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", massageBook("adjacent", "master", 1, 1))
	massageCode(t, err, http.StatusConflict, "client_conflict")
	_, err = db.Exec(t.Context(), `UPDATE core.massage_events SET daily_limit=1`)
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", massageBook("limit", "bob", 5, 1))
	massageCode(t, err, http.StatusConflict, "daily_limit")
	service.Now = func() time.Time { return start.Add(5 * time.Minute) }
	instant := massage.Command{Key: "instant", Action: "instant", Event: "sandbox-festival", Party: "night", Length: 1}
	_, err = service.Execute(t.Context(), "visitor", instant)
	massageCode(t, err, http.StatusForbidden, "forbidden")
	booking, err := service.Execute(t.Context(), "master", instant)
	require.NoError(t, err)
	assert.Equal(t, 0, booking.Slot)
	assert.Equal(t, "master", booking.Owner)
	assert.Equal(t, "master", booking.Specialist)
	_, err = service.SetPreferences(t.Context(), "master", booking.Event, massage.Preferences{})
	require.NoError(t, err)
	count, err := service.QueueReminders(t.Context(), booking.Event)
	require.NoError(t, err)
	assert.Positive(t, count)
	count, err = service.QueueReminders(t.Context(), booking.Event)
	require.NoError(t, err)
	assert.Zero(t, count)
	notices, err := service.PendingNotices(t.Context(), "master")
	require.NoError(t, err)
	for _, notice := range notices {
		assert.NotEqual(t, "next", notice.Kind)
		massageCode(t, service.AcknowledgeNotice(t.Context(), "visitor", notice.ID), http.StatusNotFound, "not_found")
		require.NoError(t, service.AcknowledgeNotice(t.Context(), "master", notice.ID))
	}
	restarted := massage.Service{DB: db}
	restored, err := restarted.Bookings(t.Context(), "master", booking.Event, "night", "mine")
	require.NoError(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, booking.ID, restored[0].ID)
}
