package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func staffFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.MassageScheduleChange, db.MassageBindingChange) {
	t.Helper()
	f, _, _, schedule := massageBookingFixture(t, 2)
	staff := ownerProfileScope(t, f.H, f.AdminID)
	args := db.MassageBindingChange{
		EventID:                 f.Event.ID,
		SpecialistID:            "alex",
		UserID:                  staff.UserID,
		Active:                  true,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: 1,
	}
	bindingChange(t, f, staff, args)
	args.ExpectedVersion = 1
	return f, staff, schedule, args
}

func staffQuery(f paymentFixture, schedule db.MassageScheduleChange) db.MassageStaffQuery {
	return db.MassageStaffQuery{
		EventID:  f.Event.ID,
		WindowID: "night",
		From:     schedule.Document.Windows[0].StartsAt,
		Until:    schedule.Document.Windows[0].EndsAt,
	}
}

func TestMassageStaffWithoutPassSharesEventTimetableAndPrivateOwnClients(t *testing.T) {
	t.Parallel()
	f, staff, schedule, binding := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	other := ownerProfileScope(t, f.H, f.AmbassadorID)
	var passes int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registrations WHERE user_id=$1", staff.UserID).
			Scan(&passes),
	)
	require.Zero(t, passes)
	_, err := f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_users SET profile='{"first_name":"Alex","last_name":"Client","secret":"not for staff"}' WHERE id=$1`, owner.UserID)
	require.NoError(t, err)
	first, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	args := massageBookingArgs(f, schedule, 0)
	args.Selection.SpecialistID = "beth"
	second, _ := bookMassage(t, f, other, args)
	events, err := f.H.store.MassageStaffEvents(t.Context(), staff, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	require.Equal(t, f.Event.ID, events.Events[0].ID)
	view, err := f.H.store.MassageStaffSchedule(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"alex"}, view.OwnSpecialistIDs)
	require.Equal(t, schedule.Document.Windows[0].StartsAt.Unix(), view.Document.Windows[0].StartsAt.Unix())
	require.Equal(t, f.Event.Document.Timezone, view.Timezone)
	query := staffQuery(f, schedule)
	page, err := f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 2)
	byID := map[uuid.UUID]db.MassageStaffBooking{}
	for _, b := range page.Bookings {
		byID[b.ID] = b
	}
	require.Equal(t, first.Snapshot.Terms, byID[first.ID].Terms)
	require.Equal(t, "Alex Client", byID[first.ID].ClientName)
	require.Equal(t, "tg://user?id=11002", byID[first.ID].Contact)
	require.Empty(t, byID[second.ID].Contact, "Event-wide timetable must not export other specialists' client contacts")
	raw, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "not for staff")
	require.NotContains(t, string(raw), "user_id")
	require.NotContains(t, string(raw), "zitadel")
	query.OwnOnly = true
	own, err := f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, own.Bookings, 1)
	require.Equal(t, first.ID, own.Bookings[0].ID)
	detail, err := f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, own.Bookings[0], detail)
	_, err = f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, second.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.MassageStaffBookings(t.Context(), owner, query)
	require.ErrorIs(t, err, pgx.ErrNoRows, "An assigned pass grants no specialist rights")
	binding.SpecialistID = "beth"
	binding.ExpectedVersion = 0
	bindingChange(t, f, staff, binding)
	own, err = f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, own.Bookings, 2)
	require.Equal(t, []string{"alex", "beth"}, own.OwnSpecialistIDs)
	_, err = f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, second.ID)
	require.NoError(t, err)
	events, err = f.H.store.MassageStaffEvents(t.Context(), staff, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, events.Events, 1, "Several aliases must not duplicate event discovery")
}

func TestMassageStaffReadSurvivesModuleDisableButNotAccessRevocation(t *testing.T) {
	t.Parallel()
	f, staff, schedule, binding := staffFixture(t)
	query := staffQuery(f, schedule)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage'),version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.MassageStaffSchedule(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	events, err := f.H.store.MassageStaffEvents(t.Context(), staff, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	foreign := staff
	foreign.Organization = "another-org"
	_, err = f.H.store.MassageStaffBookings(t.Context(), foreign, query)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	foreign = staff
	foreign.BotID++
	_, err = f.H.store.MassageStaffSchedule(t.Context(), foreign, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	binding.ExpectedEventVersion++
	binding.Active = false
	bindingChange(t, f, staff, binding)
	_, err = f.H.store.MassageStaffSchedule(t.Context(), staff, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, uuid.New())
	require.ErrorIs(t, err, pgx.ErrNoRows)
	events, err = f.H.store.MassageStaffEvents(t.Context(), staff, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, events.Events)
	binding.Active = true
	binding.ExpectedVersion = 2
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
	require.NoError(t, err)
	_, err = f.H.store.MassageStaffEvents(t.Context(), staff, uuid.Nil, 20)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestMiniAppMassageStaffSharesReadsOnOneConnectionAndRejectsExpiredSession(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	session := nativeMassageSession(t, f, staff, 11001)
	deployment := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
	dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	values := dsn.Query()
	values.Set("pool_max_conns", "1")
	dsn.RawQuery = values.Encode()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store, err := db.Open(ctx, dsn.String())
	require.NoError(t, err)
	defer store.Close()
	booking, _ := bookMassage(t, f, ownerProfileScope(t, f.H, f.OwnerID), massageBookingArgs(f, schedule, 0))
	agent, err := store.MassageStaffBookings(ctx, staff, staffQuery(f, schedule))
	require.NoError(t, err)
	native, err := store.MiniAppMassageStaffBookings(ctx, deployment, session, staffQuery(f, schedule))
	require.NoError(t, err)
	require.Equal(t, agent.Bookings, native.Bookings)
	events, err := store.MiniAppMassageStaffEvents(ctx, deployment, session, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	view, err := store.MiniAppMassageStaffSchedule(ctx, deployment, session, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"alex"}, view.OwnSpecialistIDs)
	detail, err := store.MiniAppMassageStaffClient(ctx, deployment, session, f.Event.ID, booking.ID)
	require.NoError(t, err)
	require.Equal(t, agent.Bookings[0], detail)
	invalid := session
	invalid.Generation[0] ^= 1
	_, err = store.MiniAppMassageStaffBookings(ctx, deployment, invalid, staffQuery(f, schedule))
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	foreign := deployment
	foreign.Organization = "another-org"
	_, err = store.MiniAppMassageStaffEvents(ctx, foreign, session, uuid.Nil, 20)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1", staff.UserID)
	require.NoError(t, err)
	_, err = store.MiniAppMassageStaffEvents(ctx, deployment, session, uuid.Nil, 20)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = store.MiniAppMassageStaffSchedule(ctx, deployment, session, f.Event.ID)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = store.MiniAppMassageStaffBookings(ctx, deployment, session, staffQuery(f, schedule))
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = store.MiniAppMassageStaffClient(ctx, deployment, session, f.Event.ID, booking.ID)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}

func TestMassageStaffRechecksGrantAndSessionAfterEventLockWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"agent-grant", "agent-identity", "native-grant", "native-logout", "native-identity"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, staff, schedule, _ := staffFixture(t)
			session := nativeMassageSession(t, f, staff, 11001)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			blocker, err := f.H.store.Pool().Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback(ctx) }()
			_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				var e error
				if strings.HasPrefix(kind, "agent-") {
					_, e = f.H.store.MassageStaffBookings(ctx, staff, staffQuery(f, schedule))
				} else {
					_, e = f.H.store.MiniAppMassageStaffBookings(
						ctx,
						db.EventScope{BotID: staff.BotID, Organization: staff.Organization},
						session,
						staffQuery(f, schedule),
					)
				}
				done <- e
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				e := f.H.store.Pool().
					QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocker.Conn().PgConn().PID()).
					Scan(&waiting)
				return e == nil && waiting
			}, 3*time.Second, 5*time.Millisecond)
			switch {
			case strings.HasSuffix(kind, "grant"):
				_, err = blocker.Exec(
					ctx,
					"UPDATE shuttle_massage_specialist_bindings SET active=false,version=version+1 WHERE event_id=$1",
					f.Event.ID,
				)
			case strings.HasSuffix(kind, "identity"):
				_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
			default:
				err = f.H.store.LogoutMiniAppSession(ctx, staff.BotID, session.Bearer, session.Generation)
			}
			require.NoError(t, err)
			require.NoError(t, blocker.Commit(ctx))
			select {
			case err = <-done:
				if strings.HasPrefix(kind, "native-") && !strings.HasSuffix(kind, "grant") {
					require.ErrorIs(t, err, db.ErrMiniAppDenied)
				} else {
					require.ErrorIs(t, err, pgx.ErrNoRows)
				}
			case <-ctx.Done():
				t.Fatal("Staff read did not finish after event lock release")
			}
		})
	}
}

func TestMassageStaffChronologicalCursorFiltersCancellationAndRangeOverlap(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	first, _ := bookMassage(t, f, ownerProfileScope(t, f.H, f.OwnerID), massageBookingArgs(f, schedule, 0))
	args := massageBookingArgs(f, schedule, 0)
	args.Selection.SpecialistID = "beth"
	second, _ := bookMassage(t, f, ownerProfileScope(t, f.H, f.AmbassadorID), args)
	query := staffQuery(f, schedule)
	query.Limit = 1
	page, err := f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 1)
	require.NotNil(t, page.Next)
	require.Equal(t, first.Snapshot.Terms.StartsAt, page.Bookings[0].Terms.StartsAt)
	query.After = page.Next
	next, err := f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, next.Bookings, 1)
	require.Nil(t, next.Next)
	require.NotEqual(t, page.Bookings[0].ID, next.Bookings[0].ID)
	for _, changed := range []string{"event", "window", "range", "ownership"} {
		wrong := query
		switch changed {
		case "event":
			wrong.EventID = uuid.New()
		case "window":
			wrong.WindowID = "other"
		case "range":
			wrong.From = wrong.From.Add(time.Minute)
		case "ownership":
			wrong.OwnOnly = true
		}
		_, err = f.H.store.MassageStaffBookings(t.Context(), staff, wrong)
		require.ErrorIs(t, err, massage.ErrInvalid)
	}
	query.After = nil
	query.From = query.From.Add(time.Minute)
	query.Until = query.From.Add(time.Minute)
	query.Limit = 20
	page, err = f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 2, "Include bookings starting before the requested visible range")
	query = staffQuery(f, schedule)
	query.Until = query.From.Add(25 * time.Hour)
	_, err = f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.ErrorIs(t, err, massage.ErrInvalid)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	cancellation := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       first.ID,
		ExpectedVersion: first.Version,
	}
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, cancellation)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, prep.ID, cancellation)
	require.NoError(t, err)
	page, err = f.H.store.MassageStaffBookings(t.Context(), staff, staffQuery(f, schedule))
	require.NoError(t, err)
	require.Len(t, page.Bookings, 1)
	require.Equal(t, second.ID, page.Bookings[0].ID)
	_, err = f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, first.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestMassageStaffRetainsPastBookedClientsAndFrozenTerms(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	booking, _ := bookMassage(t, f, ownerProfileScope(t, f.H, f.OwnerID), massageBookingArgs(f, schedule, 0))
	// Represent a retained completed event; no wall-clock sleeps or payment effects.
	delta := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour).Sub(booking.Snapshot.Terms.StartsAt)
	terms := &booking.Snapshot.Terms
	terms.StartsAt = terms.StartsAt.Add(delta)
	terms.TreatmentEndsAt = terms.TreatmentEndsAt.Add(delta)
	terms.OccupancyEndsAt = terms.OccupancyEndsAt.Add(delta)
	terms.WindowStartsAt = terms.WindowStartsAt.Add(delta)
	terms.WindowEndsAt = terms.WindowEndsAt.Add(delta)
	raw, err := json.Marshal(booking.Snapshot)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_bookings SET starts_at=$2,treatment_ends_at=$3,occupancy_ends_at=$4,snapshot=$5 WHERE id=$1", booking.ID, terms.StartsAt, terms.TreatmentEndsAt, terms.OccupancyEndsAt, raw)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET starts_at=$2,ends_at=$3 WHERE id=$1", f.Event.ID, terms.WindowStartsAt, terms.WindowEndsAt)
	require.NoError(t, err)
	query := db.MassageStaffQuery{
		EventID:  f.Event.ID,
		WindowID: "night",
		From:     terms.WindowStartsAt,
		Until:    terms.WindowEndsAt,
		OwnOnly:  true,
	}
	page, err := f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 1)
	require.True(t, page.Bookings[0].Terms.TreatmentEndsAt.Before(time.Now()))
	require.Equal(t, *terms, page.Bookings[0].Terms)
	_, err = f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, booking.ID)
	require.NoError(t, err)

	// The editable schedule still has the future party. Historical discovery must
	// retain the distinct old range even when its public ID has been reused.
	windows, err := f.H.store.MassageStaffWindows(t.Context(), staff, db.MassageStaffWindowsQuery{EventID: f.Event.ID})
	require.NoError(t, err)
	require.Len(t, windows.Windows, 2)
	require.True(t, terms.WindowStartsAt.Equal(windows.Windows[0].StartsAt))
	require.True(t, terms.WindowEndsAt.Equal(windows.Windows[0].EndsAt))
	require.Equal(t, terms.WindowName, windows.Windows[0].Name)
	require.Equal(t, "night", windows.Windows[1].ID)
	// Changing event dates must not remove the retained client's timetable/detail.
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET starts_at=$2,ends_at=$3 WHERE id=$1", f.Event.ID, schedule.Document.Windows[0].StartsAt, schedule.Document.Windows[0].EndsAt)
	require.NoError(t, err)
	page, err = f.H.store.MassageStaffBookings(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 1)
	_, err = f.H.store.MassageStaffClient(t.Context(), staff, f.Event.ID, booking.ID)
	require.NoError(t, err)
	// A shortened current window is a separate navigation choice; removing the
	// old public ID entirely must not hide its booked frozen window either.
	for _, removed := range []bool{false, true} {
		current := schedule.Document
		current.Windows[0].StartsAt = current.Windows[0].StartsAt.Add(time.Minute)
		if removed {
			current.Windows[0].ID = "replacement"
		}
		raw, err = json.Marshal(current)
		require.NoError(t, err)
		_, err = f.H.store.Pool().
			Exec(t.Context(), "UPDATE shuttle_massage_schedules SET document=$2 WHERE event_id=$1", f.Event.ID, raw)
		require.NoError(t, err)
		windows, err = f.H.store.MassageStaffWindows(
			t.Context(),
			staff,
			db.MassageStaffWindowsQuery{EventID: f.Event.ID},
		)
		require.NoError(t, err)
		require.Len(t, windows.Windows, 2)
		require.Equal(t, "night", windows.Windows[0].ID)
		require.True(t, terms.WindowStartsAt.Equal(windows.Windows[0].StartsAt))
	}
}

func TestMassageStaffPageByteBoundContinuesWithoutDroppingRows(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	booking, _ := bookMassage(t, f, ownerProfileScope(t, f.H, f.OwnerID), massageBookingArgs(f, schedule, 0))
	label := massage.ScheduleLabel{EN: strings.Repeat("<", 128), RU: strings.Repeat("<", 128)}
	booking.Snapshot.Terms.WindowName = label
	booking.Snapshot.Terms.SpecialistName = label
	booking.Snapshot.Terms.ServiceName = label
	raw, err := json.Marshal(booking.Snapshot)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_bookings SET snapshot=$2 WHERE id=$1", booking.ID, raw)
	require.NoError(t, err)
	// A dense historical fixture exercises response bounds independently of booking
	// capacity. Each copied row has its own preparation and booking identity.
	for range 19 {
		prep, id := uuid.New(), uuid.New()
		_, err = f.H.store.Pool().
			Exec(t.Context(), `INSERT INTO shuttle_massage_preparations(id,event_id,user_id,operation_id,kind,source,request_hash,arguments,snapshot,expires_at)
   SELECT $2,event_id,user_id,$2,kind,source,request_hash,arguments,snapshot,expires_at FROM shuttle_massage_preparations WHERE id=(SELECT preparation_id FROM shuttle_massage_bookings WHERE id=$1)`, booking.ID, prep)
		require.NoError(t, err)
		_, err = f.H.store.Pool().
			Exec(t.Context(), `INSERT INTO shuttle_massage_bookings(id,event_id,user_id,preparation_id,window_id,specialist_id,service_id,starts_at,treatment_ends_at,occupancy_ends_at,requires_table,currency,currency_scale,price_minor,snapshot,source)
   SELECT $2,event_id,user_id,$3,window_id,specialist_id,service_id,starts_at,treatment_ends_at,occupancy_ends_at,requires_table,currency,currency_scale,price_minor,snapshot,source FROM shuttle_massage_bookings WHERE id=$1`, booking.ID, id, prep)
		require.NoError(t, err)
	}
	query := staffQuery(f, schedule)
	seen := map[uuid.UUID]bool{}
	pages := 0
	for {
		page, e := f.H.store.MassageStaffBookings(t.Context(), staff, query)
		require.NoError(t, e)
		encoded, e := json.Marshal(page)
		require.NoError(t, e)
		require.LessOrEqual(t, len(encoded), 56*1024)
		require.NotEmpty(t, page.Bookings)
		for _, b := range page.Bookings {
			require.False(t, seen[b.ID])
			seen[b.ID] = true
		}
		pages++
		require.LessOrEqual(t, pages, 20)
		if page.Next == nil {
			break
		}
		query.After = page.Next
	}
	require.Len(t, seen, 20)
	require.Greater(t, pages, 1, "Byte bound must reduce a dense escaped-label page")
}
