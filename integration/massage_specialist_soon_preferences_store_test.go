package integration_test

import (
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMassageSpecialistSoonPreferencesPreserveQueuedBookingNotice(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	args := massageBookingArgs(f, schedule, 0)
	op := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, op, args)
	require.NoError(t, err)
	booking, err := f.H.store.BookOwnMassage(t.Context(), owner, op, proposal.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	notices := specialistNotices(t, f, booking.ID)
	require.Len(t, notices, 1)
	scope := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
	for index, value := range []bool{false, true} {
		_, err = f.H.store.SetMassageSpecialistPreferences(
			t.Context(),
			staff,
			uuid.New(),
			db.MassageSpecialistPreferenceChange{
				EventID: f.Event.ID, ExpectedVersion: int64(index), NotifyNext: &value,
			},
		)
		require.NoError(t, err)
		recipient, readErr := f.H.store.MassageSpecialistNotificationRecipient(t.Context(), scope, notices[0].ID)
		require.NoError(t, readErr, "Upcoming-session preferences must not invalidate the independent booked notice")
		require.Equal(t, staff.UserID, recipient.ID)
	}
	for index, value := range []bool{false, true} {
		_, err = f.H.store.SetMassageSpecialistPreferences(
			t.Context(),
			staff,
			uuid.New(),
			noticePreference(f.Event.ID, int64(index+2), value),
		)
		require.NoError(t, err)
	}
	_, err = f.H.store.MassageSpecialistNotificationRecipient(t.Context(), scope, notices[0].ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Re-enabling booked notifications must not revive an older booked notice")
}

func TestMassageSpecialistSoonPreferencesRetainsLegacyReceipts(t *testing.T) {
	t.Parallel()
	f, staff, _, _ := staffFixture(t)
	pool := f.H.store.Pool()
	_, err := pool.Exec(
		t.Context(),
		"INSERT INTO shuttle_massage_specialist_preferences(user_id,notify_bookings,"+
			"version) VALUES($1,false,4)",
		staff.UserID,
	)
	require.NoError(t, err)
	op := uuid.New()
	args := noticePreference(f.Event.ID, 3, false)
	request, err := json.Marshal(args)
	require.NoError(t, err)
	hash := sha256.Sum256(request)
	legacy, err := json.Marshal(map[string]any{"user_id": staff.UserID, "notify_bookings": false, "version": 4})
	require.NoError(t, err)
	_, err = pool.Exec(
		t.Context(),
		"INSERT INTO shuttle_massage_specialist_preference_operations(user_id,operation_id,"+
			"event_id,source,request_hash,result) VALUES($1,$2,$3,'agent',$4,$5)",
		staff.UserID,
		op,
		f.Event.ID,
		hash[:],
		legacy,
	)
	require.NoError(t, err)
	expected := db.MassageSpecialistPreferences{
		UserID:         staff.UserID,
		NotifyBookings: false,
		NotifyNext:     true,
		Version:        4,
	}
	current, err := f.H.store.MassageSpecialistPreferences(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, expected, current)
	// A legacy JSON receipt remains immutable on the current schema.
	receipt, err := f.H.store.SetMassageSpecialistPreferences(t.Context(), staff, op, args)
	require.NoError(t, err)
	require.Equal(t, expected, receipt)
	value := false
	changed, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		db.MassageSpecialistPreferenceChange{EventID: f.Event.ID, ExpectedVersion: 4, NotifyNext: &value},
	)
	require.NoError(t, err)
	require.False(t, changed.NotifyNext)
	require.False(t, changed.NotifyBookings)
	require.EqualValues(t, 5, changed.Version)
	receipt, err = f.H.store.MassageSpecialistPreferenceOperation(t.Context(), staff, f.Event.ID, op)
	require.NoError(t, err)
	require.Equal(t, expected, receipt)
	var original []byte
	require.NoError(
		t,
		pool.QueryRow(t.Context(), "SELECT result FROM shuttle_massage_specialist_preference_operations WHERE "+
			"operation_id=$1", op).
			Scan(&original),
	)
	require.JSONEq(t, string(legacy), string(original))
	current, err = f.H.store.MassageSpecialistPreferences(t.Context(), staff, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, changed, current)
}
