package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminutilities"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestAdminUtilitiesRightsAndMetadata(t *testing.T) {
	t.Parallel()
	db, bookings := bookingFixture(t)
	service := adminutilities.Service{DB: db}
	_, err := service.User(t.Context(), "alice", 101)
	requireCode(t, err, "forbidden")
	_, err = service.Refresh(t.Context(), "alice")
	requireCode(t, err, "forbidden")
	_, err = db.Exec(t.Context(), `UPDATE core.users SET first_name='Synthetic',username='test_user' WHERE id='alice';
 INSERT INTO core.conversation_events(owner,source_key,kind,created_at) VALUES
 ('alice','old-request','user',now()-interval '2 days'),('alice','request','user',now()),
 ('alice','reply','assistant',now()),('alice','manual-command','manual',now());`)
	require.NoError(t, err)
	_, err = bookings.Execute(t.Context(), "alice", bookingCommand("solo", "diagnostics", passbooking.Booking{}))
	require.NoError(t, err)
	var id int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT telegram_id FROM core.users WHERE id='alice'`).Scan(&id))
	report, err := service.User(t.Context(), "bob", id)
	require.NoError(t, err)
	assert.True(t, report.Found)
	assert.Equal(t, "Synthetic", report.Names["first_name"])
	assert.EqualValues(t, 2, report.Requests)
	assert.EqualValues(t, 1, report.RecentRequests)
	assert.EqualValues(t, 1, report.Replies)
	assert.NotNil(t, report.LatestRequest)
	require.Len(t, report.Passes, 1)
	assert.Contains(t, string(report.Passes[0]), `"event": "dance"`)
	missing, err := service.User(t.Context(), "bob", 99999999)
	require.NoError(t, err)
	assert.False(t, missing.Found)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	// Remaining event payment rights must not grant global diagnostics.
	requireCode(t, service.Authorize(t.Context(), "bob"), "forbidden")
}

func TestAdminUtilitiesRefreshCurrentSQL(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('finished',now()-interval '1 day')`,
	)
	require.NoError(t, err)
	result, err := (adminutilities.Service{DB: db}).Refresh(t.Context(), "bob")
	require.NoError(t, err)
	assert.Equal(t, []string{"dance", "finished"}, result.All)
	assert.Equal(t, []string{"dance"}, result.Active)
}
