package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassExportSnapshotPricesAndText(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='paid',assigned_at=now(),price=CASE WHEN owner='alice' THEN 101 ELSE 100 END,tier_index=2,comment='=1+2',skip_balance=true;
 UPDATE core.pass_profiles SET legal_name='@private legal name'; UPDATE core.pass_event_tiers SET price=999`,
	)
	require.NoError(t, err)
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	file := openExport(t, body)
	rows := exportRows(t, file, "Passes")
	require.Len(t, rows, 3)
	require.Len(t, rows[0], 25)
	assert.Equal(t, "User ID", rows[0][0])
	assert.Equal(t, "101", rows[1][0])
	assert.Equal(t, "@private legal name", rows[1][5])
	assert.Equal(t, "202", rows[1][11])
	assert.Equal(t, "201", rows[1][12])
	assert.Equal(t, "101", rows[1][13])
	assert.Equal(t, "3", rows[1][14])
	assert.Equal(t, "202", rows[1][18], "legacy paid receiver falls back to current admin")
	assert.Equal(t, "=1+2", rows[1][24])
	formula, err := file.GetCellFormula("Passes", "Y2")
	require.NoError(t, err)
	assert.Empty(t, formula)
	panes, err := file.GetPanes("Passes")
	require.NoError(t, err)
	assert.True(t, panes.Freeze)
}

func TestPassExportEventScopedRights(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('other',now()+interval '1 day'),('finished',now()-interval '1 day');
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 SELECT id,'alice',1,'waitlist','leader','solo','bob',now() FROM core.pass_events;
 DELETE FROM core.pass_booking_admins; UPDATE core.pass_payment_admins SET hidden=true`,
	)
	require.NoError(t, err)
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rows := exportRows(t, openExport(t, body), "Passes")
	require.Len(t, rows, 2)
	assert.Equal(t, "dance", rows[1][7])
	_, err = service.Export(t.Context(), "alice")
	requireCode(t, err, "forbidden")
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	body, err = service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rows = exportRows(t, openExport(t, body), "Passes")
	require.Len(t, rows, 3)
	assert.Equal(t, "other", rows[2][7])
	_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='bob'`)
	require.NoError(t, err)
	_, err = service.Export(t.Context(), "bob")
	requireCode(t, err, "forbidden")
}

func TestPassExportOmitsCancelledRegistrations(t *testing.T) {
	t.Parallel()
	_, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", bookingCommand("cancel", "cancel", alice))
	require.NoError(t, err)
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	assert.Len(t, exportRows(t, openExport(t, body), "Passes"), 1, "cancelled registration leaves only the header")
}

func TestPassExportLimitsFailClosed(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "one", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES($1,now()+interval '1 day');`,
		strings.Repeat("x", 33000),
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES($1,'alice',1,'waitlist','leader','solo','bob',now())`,
		strings.Repeat("x", 33000),
	)
	require.NoError(t, err)
	body, err := service.Export(t.Context(), "bob")
	requireCode(t, err, "export_too_large")
	assert.Empty(t, body)
}
