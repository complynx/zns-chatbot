package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func noticePreference(event uuid.UUID, version int64, enabled bool) db.MassageSpecialistPreferenceChange {
	return db.MassageSpecialistPreferenceChange{EventID: event, ExpectedVersion: version, NotifyBookings: &enabled}
}

type specialistNoticeRow struct {
	ID, Recipient                              uuid.UUID
	Kind, State, ClientName                    string
	BindingVersion, PreferenceVersion, Version int64
}

func specialistNotices(t *testing.T, f paymentFixture, booking uuid.UUID) []specialistNoticeRow {
	t.Helper()
	rows, err := f.H.store.Pool().
		Query(t.Context(), `SELECT id,recipient_id,kind,state,client_name,binding_version,preference_version,version
 FROM shuttle_massage_specialist_notifications WHERE booking_id=$1 ORDER BY kind`, booking)
	require.NoError(t, err)
	defer rows.Close()
	result := []specialistNoticeRow{}
	for rows.Next() {
		var n specialistNoticeRow
		require.NoError(
			t,
			rows.Scan(
				&n.ID,
				&n.Recipient,
				&n.Kind,
				&n.State,
				&n.ClientName,
				&n.BindingVersion,
				&n.PreferenceVersion,
				&n.Version,
			),
		)
		result = append(result, n)
	}
	require.NoError(t, rows.Err())
	return result
}

func TestMassageSpecialistPreferencesShareNativeAgentCASAndOriginalReceipts(t *testing.T) {
	t.Parallel()
	f, staff, _, _ := staffFixture(t)
	deployment := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
	session := nativeMassageSession(t, f, staff, 11001)
	initial, err := f.H.store.MassageSpecialistPreferences(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	require.Equal(
		t,
		db.MassageSpecialistPreferences{UserID: staff.UserID, NotifyBookings: true, NotifyNext: true},
		initial,
	)
	same, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		noticePreference(f.Event.ID, 0, true),
	)
	require.NoError(t, err)
	require.Equal(t, initial, same)
	op := uuid.New()
	disabled, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		op,
		noticePreference(f.Event.ID, 0, false),
	)
	require.NoError(t, err)
	require.False(t, disabled.NotifyBookings)
	require.EqualValues(t, 1, disabled.Version)
	replay, err := f.H.store.SetMiniAppMassageSpecialistPreferences(
		t.Context(),
		deployment,
		session,
		op,
		noticePreference(f.Event.ID, 0, false),
	)
	require.NoError(t, err)
	require.Equal(t, disabled, replay)
	enabled, err := f.H.store.SetMiniAppMassageSpecialistPreferences(
		t.Context(),
		deployment,
		session,
		uuid.New(),
		noticePreference(f.Event.ID, 1, true),
	)
	require.NoError(t, err)
	require.True(t, enabled.NotifyBookings)
	require.EqualValues(t, 2, enabled.Version)
	replay, err = f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		op,
		noticePreference(f.Event.ID, 0, false),
	)
	require.NoError(t, err)
	require.Equal(t, disabled, replay, "A transport replay is historical, not a new preference change")
	recovered, err := f.H.store.MiniAppMassageSpecialistPreferenceOperation(
		t.Context(),
		deployment,
		session,
		f.Event.ID,
		op,
	)
	require.NoError(t, err)
	require.Equal(t, disabled, recovered)
	current, err := f.H.store.MiniAppMassageSpecialistPreferences(t.Context(), deployment, session, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, enabled, current)
	_, err = f.H.store.SetMassageSpecialistPreferences(t.Context(), staff, op, noticePreference(f.Event.ID, 0, true))
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		noticePreference(f.Event.ID, 1, false),
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		db.MassageSpecialistPreferenceChange{EventID: f.Event.ID, ExpectedVersion: 2},
	)
	require.ErrorIs(t, err, massage.ErrInvalid)
}

func anotherStaffEvent(t *testing.T, f paymentFixture) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.H.store.Pool().Exec(t.Context(), "INSERT INTO shuttle_events\n (id,bot_id,organization_id,name,timezone,sta" +
"rts_at,ends_at,modules,version)\n SELECT $1,bot_id,organization_id,name,t" +
"imezone,starts_at,ends_at,modules,version FROM shuttle_events WHERE id=$" +
"2", id, f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_massage_schedules SELECT $1,version,document FROM sh" +
"uttle_massage_schedules WHERE event_id=$2", id, f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_massage_specialist_bindings(event_id,specialist_id,u" +
"ser_id,active,version,updated_by)\n SELECT $1,specialist_id,user_id,activ" +
"e,version,updated_by FROM shuttle_massage_specialist_bindings WHERE even" +
"t_id=$2", id, f.Event.ID)
	require.NoError(t, err)
	return id
}

func TestMassageSpecialistPreferencesSerializeAcrossEventsAndSurviveRegrant(t *testing.T) {
	t.Parallel()
	f, staff, _, binding := staffFixture(t)
	second := anotherStaffEvent(t, f)
	deployment := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
	session := nativeMassageSession(t, f, staff, 11001)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := f.H.store.SetMassageSpecialistPreferences(
			ctx,
			staff,
			uuid.New(),
			noticePreference(f.Event.ID, 0, false),
		)
		results <- err
	}()
	go func() {
		<-start
		_, err := f.H.store.SetMiniAppMassageSpecialistPreferences(
			ctx,
			deployment,
			session,
			uuid.New(),
			noticePreference(second, 0, false),
		)
		results <- err
	}()
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, db.ErrConflict):
			conflicts++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	for _, event := range []uuid.UUID{f.Event.ID, second} {
		current, err := f.H.store.MassageSpecialistPreferences(t.Context(), staff, event)
		require.NoError(t, err)
		require.False(t, current.NotifyBookings)
		require.EqualValues(t, 1, current.Version)
	}
	binding.Active = false
	revoked := bindingChange(t, f, staff, binding)
	_, err := f.H.store.MassageSpecialistPreferences(t.Context(), staff, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	binding.Active = true
	binding.ExpectedVersion = revoked.Version
	bindingChange(t, f, staff, binding)
	retained, err := f.H.store.MassageSpecialistPreferences(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	require.False(t, retained.NotifyBookings)
	require.EqualValues(t, 1, retained.Version)
}

func TestMassageSpecialistPreferencesDenyForeignRevokedAndExpiredSession(t *testing.T) {
	t.Parallel()
	f, staff, _, _ := staffFixture(t)
	other := ownerProfileScope(t, f.H, f.OwnerID)
	wrongBot, wrongOrg := staff, staff
	wrongBot.BotID++
	wrongOrg.Organization = "another-org"
	for _, actor := range []db.ProfileScope{other, wrongBot, wrongOrg} {
		_, err := f.H.store.MassageSpecialistPreferences(t.Context(), actor, f.Event.ID)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		_, err = f.H.store.SetMassageSpecialistPreferences(
			t.Context(),
			actor,
			uuid.New(),
			noticePreference(f.Event.ID, 0, false),
		)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	session := nativeMassageSession(t, f, staff, 11001)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interva" +
"l '1 second' WHERE user_id=$1", staff.UserID)
	require.NoError(t, err)
	_, err = f.H.store.SetMiniAppMassageSpecialistPreferences(
		t.Context(),
		db.EventScope{BotID: staff.BotID, Organization: staff.Organization},
		session,
		uuid.New(),
		noticePreference(f.Event.ID, 0, false),
	)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
	require.NoError(t, err)
	_, err = f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		noticePreference(f.Event.ID, 0, false),
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_specialist_preference_operations").
			Scan(&count),
	)
	require.Zero(t, count)
}

func TestMassageSpecialistNoticesShareBookingCancelAndReplayAcrossInterfaces(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{false, true} {
		name := "agent"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, staff, schedule, _ := staffFixture(t)
			owner := ownerProfileScope(t, f.H, f.OwnerID)
			_, err := f.H.store.Pool().
				Exec(t.Context(), `UPDATE shuttle_user_profiles SET full_name='Client <untrusted>' WHERE user_id=$1`, owner.UserID)
			require.NoError(t, err)
			deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
			session := nativeMassageSession(t, f, owner, 11002)
			args := massageBookingArgs(f, schedule, 0)
			op := uuid.New()
			proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, op, args)
			require.NoError(t, err)
			var booking db.MassageBooking
			if native {
				booking, err = f.H.store.BookMiniAppMassage(t.Context(), deployment, session, op, proposal.ID, args)
			} else {
				booking, err = f.H.store.BookOwnMassage(
					t.Context(),
					owner,
					op,
					proposal.ID,
					args,
					db.MassageSourceAgent,
				)
			}
			require.NoError(t, err)
			before := specialistNotices(t, f, booking.ID)
			require.Len(t, before, 1)
			require.Equal(t, staff.UserID, before[0].Recipient)
			require.EqualValues(t, 1, before[0].BindingVersion)
			require.Zero(t, before[0].PreferenceVersion)
			require.Equal(t, "booked", before[0].Kind)
			require.Equal(t, "pending", before[0].State)
			_, err = f.H.store.BookOwnMassage(t.Context(), owner, uuid.New(), proposal.ID, args, db.MassageSourceAgent)
			require.NoError(t, err)
			require.Equal(t, before, specialistNotices(t, f, booking.ID))
			cancelArgs := db.MassageCancellationArguments{
				EventID:         f.Event.ID,
				BookingID:       booking.ID,
				ExpectedVersion: 1,
			}
			cancelOp := uuid.New()
			prepared, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, cancelOp, cancelArgs)
			require.NoError(t, err)
			if native {
				_, err = f.H.store.CancelMiniAppMassage(
					t.Context(),
					deployment,
					session,
					cancelOp,
					prepared.ID,
					cancelArgs,
				)
			} else {
				_, err = f.H.store.CancelOwnMassage(t.Context(), owner, cancelOp, prepared.ID, cancelArgs)
			}
			require.NoError(t, err)
			after := specialistNotices(t, f, booking.ID)
			require.Len(t, after, 2)
			require.Equal(t, before[0].ID, after[0].ID)
			require.Equal(t, "delivery_blocked", after[0].State)
			require.EqualValues(t, 2, after[0].Version)
			require.Equal(t, "cancelled", after[1].Kind)
			require.Equal(t, "pending", after[1].State)
			require.Equal(t, staff.UserID, after[1].Recipient)
			_, err = f.H.store.CancelOwnMassage(t.Context(), owner, uuid.New(), prepared.ID, cancelArgs)
			require.NoError(t, err)
			require.Equal(t, after, specialistNotices(t, f, booking.ID))
		})
	}
}

func TestMassageSpecialistNoticesNeverBackfillUnboundOrMutedTransitions(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 1)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	old, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	require.Empty(t, specialistNotices(t, f, old.ID))
	binding := db.MassageBindingChange{
		EventID:                 f.Event.ID,
		SpecialistID:            "alex",
		UserID:                  admin.UserID,
		Active:                  true,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: 1,
	}
	bindingChange(t, f, admin, binding)
	require.Empty(t, specialistNotices(t, f, old.ID))
	_, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		admin,
		uuid.New(),
		noticePreference(f.Event.ID, 0, false),
	)
	require.NoError(t, err)
	muted, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 1))
	require.Empty(t, specialistNotices(t, f, muted.ID))
	_, err = f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		admin,
		uuid.New(),
		noticePreference(f.Event.ID, 1, true),
	)
	require.NoError(t, err)
	require.Empty(t, specialistNotices(t, f, muted.ID))
	current, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 2))
	notices := specialistNotices(t, f, current.ID)
	require.Len(t, notices, 1)
	require.EqualValues(t, 2, notices[0].PreferenceVersion)
}

func TestMassageSpecialistNoticeFailureRollsBackBookingOrCancellation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"booked", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, _, schedule, _ := staffFixture(t)
			owner := ownerProfileScope(t, f.H, f.OwnerID)
			args := massageBookingArgs(f, schedule, 0)
			var booking db.MassageBooking
			var before []specialistNoticeRow
			if kind == "cancelled" {
				booking, _ = bookMassage(t, f, owner, args)
				before = specialistNotices(t, f, booking.ID)
			}
			_, err := f.H.store.Pool().
				Exec(t.Context(), "CREATE FUNCTION reject_specialist_notice() RETURNS trigger LANGUAGE plpg" +
"sql AS $$ BEGIN RAISE EXCEPTION 'synthetic specialist notice failure'; E" +
"ND $$;\n CREATE TRIGGER reject_specialist_notice BEFORE INSERT ON shuttle" +
"_massage_specialist_notifications FOR EACH ROW EXECUTE FUNCTION reject_s" +
"pecialist_notice()")
			require.NoError(t, err)
			op := uuid.New()
			if kind == "booked" {
				proposal, e := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, op, args)
				require.NoError(t, e)
				_, err = f.H.store.BookOwnMassage(t.Context(), owner, op, proposal.ID, args, db.MassageSourceAgent)
			} else {
				cancelArgs := db.MassageCancellationArguments{
					EventID:         f.Event.ID,
					BookingID:       booking.ID,
					ExpectedVersion: 1,
				}
				proposal, e := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, cancelArgs)
				require.NoError(t, e)
				_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, proposal.ID, cancelArgs)
			}
			require.ErrorContains(t, err, "synthetic specialist notice failure")
			var receipts int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_operations WHERE operation_id=$1", op).
					Scan(&receipts),
			)
			require.Zero(t, receipts)
			if kind == "booked" {
				var count int
				require.NoError(
					t,
					f.H.store.Pool().
						QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_bookings").
						Scan(&count),
				)
				require.Zero(t, count)
			} else {
				current, e := f.H.store.OwnMassageBooking(t.Context(), owner, f.Event.ID, booking.ID)
				require.NoError(t, e)
				require.Equal(t, booking, current)
				require.Equal(t, before, specialistNotices(t, f, booking.ID))
			}
		})
	}
}

func TestMassageSpecialistNoticePreferencesRemainReadable(t *testing.T) {
	t.Parallel()
	f, staff, _, _ := staffFixture(t)
	_, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		noticePreference(f.Event.ID, 0, true),
	)
	require.NoError(t, err)
	current, err := f.H.store.MassageSpecialistPreferences(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	require.True(t, current.NotifyBookings)
	require.Zero(t, current.Version)
}
