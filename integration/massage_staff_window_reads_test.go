package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestMassageStaffWindowDiscoveryPagesVersionsAndRechecksAccess(t *testing.T) {
	t.Parallel()
	f, staff, schedule, binding := staffFixture(t)
	document := schedule.Document
	document.Windows = nil
	for i := range 25 {
		window := schedule.Document.Windows[0]
		window.ID = fmt.Sprintf("party_%02d", i)
		window.StartsAt = window.StartsAt.Add(time.Duration(i) * time.Hour)
		window.EndsAt = window.StartsAt.Add(time.Hour)
		document.Windows = append(document.Windows, window)
	}
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_schedules SET document=$2 WHERE event_id=$1", f.Event.ID, raw)
	require.NoError(t, err)
	query := db.MassageStaffWindowsQuery{EventID: f.Event.ID}
	first, err := f.H.store.MassageStaffWindows(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, first.Windows, 20)
	require.NotNil(t, first.Next)
	require.Equal(t, "party_00", first.Windows[0].ID)
	require.Equal(t, "party_19", first.Windows[19].ID)
	query.After = first.Next
	next, err := f.H.store.MassageStaffWindows(t.Context(), staff, query)
	require.NoError(t, err)
	require.Len(t, next.Windows, 5)
	require.Nil(t, next.Next)
	require.Equal(t, "party_20", next.Windows[0].ID)
	require.Equal(t, "party_24", next.Windows[4].ID)
	invalid := query
	invalid.EventID = uuid.New()
	_, err = f.H.store.MassageStaffWindows(t.Context(), staff, invalid)
	require.ErrorIs(t, err, massage.ErrInvalid)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	_, err = f.H.store.MassageStaffWindows(t.Context(), owner, query)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	binding.Active = false
	bindingChange(t, f, staff, binding)
	_, err = f.H.store.MassageStaffWindows(t.Context(), staff, query)
	require.ErrorIs(t, err, pgx.ErrNoRows, "A cursor cannot retain a revoked grant")
}
