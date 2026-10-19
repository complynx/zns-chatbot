package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const administrativeMassagePath = "/internal/agent/administration/massage_bookings"

func readAdministrativeMassage(
	t *testing.T,
	f paymentFixture,
	after uuid.UUID,
	limit int,
) db.MassageAdministrationPage {
	t.Helper()
	arguments := map[string]any{"event_id": f.Event.ID, "limit": limit}
	if after != uuid.Nil {
		arguments["after"] = after
	}
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, administrativeMassagePath, map[string]any{
		"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": arguments,
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.LessOrEqual(t, len(raw), 64<<10)
	var response struct {
		Status string                       `json:"status"`
		Result db.MassageAdministrationPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Equal(t, "succeeded", response.Status)
	require.Equal(t, f.Event.ID, response.Result.EventID)
	require.Equal(t, f.Event.Document.Timezone, response.Result.Timezone)
	require.False(t, response.Result.ObservedAt.IsZero())
	return response.Result
}

func TestMassageAdministrationReadIncludesOwnersAndHistoryWithPagination(t *testing.T) {
	t.Parallel()
	f, owner, other, schedule := massageBookingFixture(t, 2)
	first, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	secondArgs := massageBookingArgs(f, schedule, 0)
	secondArgs.Selection.SpecialistID = "beth"
	second, _ := bookMassage(t, f, other, secondArgs)
	cancel := db.MassageCancellationArguments{EventID: f.Event.ID, BookingID: first.ID, ExpectedVersion: first.Version}
	op := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, cancel)
	require.NoError(t, err)
	first, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, proposal.ID, cancel)
	require.NoError(t, err)
	// Disabling new bookings does not hide retained history from an administrator.
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY[]::text[] WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	page := readAdministrativeMassage(t, f, uuid.Nil, 1)
	require.Len(t, page.Bookings, 1)
	require.NotNil(t, page.NextAfter)
	next := readAdministrativeMassage(t, f, *page.NextAfter, 1)
	require.Len(t, next.Bookings, 1)
	require.Nil(t, next.NextAfter)
	all := append([]db.MassageAdministrationBooking{}, page.Bookings...)
	all = append(all, next.Bookings...)
	expected := map[uuid.UUID]db.MassageAdministrationBooking{
		first.ID: {UserID: owner.UserID, Booking: first}, second.ID: {UserID: other.UserID, Booking: second},
	}
	for _, item := range all {
		require.Equal(t, expected[item.Booking.ID], item)
		delete(expected, item.Booking.ID)
	}
	require.Empty(t, expected)
	own, err := f.H.store.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, own.Bookings, 1, "Administrative listing must not expand ordinary owner reads")
	require.Equal(t, first, own.Bookings[0])
}

func TestMassageAdministrationReadRequiresGlobalAdminAndDeploymentEvent(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 1)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	request := map[string]any{
		"protocol_version": 1,
		"operation_id":     uuid.NewString(),
		"arguments":        map[string]any{"event_id": f.Event.ID},
	}
	for _, token := range []string{f.Owner, f.Ambassador} {
		code, raw := adminRequest(t, f.H, token, http.MethodPost, administrativeMassagePath, request)
		require.Equal(t, http.StatusForbidden, code, string(raw))
		require.NotContains(t, string(raw), booking.ID.String())
	}
	for _, query := range []string{
		"UPDATE shuttle_events SET organization_id='foreign-org' WHERE id=$1",
		"UPDATE shuttle_events SET bot_id=bot_id+1000 WHERE id=$1",
	} {
		_, err := f.H.store.Pool().
			Exec(t.Context(), "UPDATE shuttle_events SET organization_id=$2,bot_id=$3 WHERE id=$1", f.Event.ID, owner.Organization, owner.BotID)
		require.NoError(t, err)
		_, err = f.H.store.Pool().Exec(t.Context(), query, f.Event.ID)
		require.NoError(t, err)
		code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, administrativeMassagePath, request)
		require.Equal(t, http.StatusNotFound, code, string(raw))
		require.NotContains(t, string(raw), booking.ID.String())
	}
	var state string
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT state FROM shuttle_massage_bookings WHERE id=$1", booking.ID).
			Scan(&state),
	)
	require.Equal(t, "booked", state)
}

func TestMassageAdministrationReadBoundsPagesWithoutDroppingRecords(t *testing.T) {
	t.Parallel()
	f, owner, other, schedule := massageBookingFixture(t, 2)
	one, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	two, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 1))
	args := massageBookingArgs(f, schedule, 0)
	args.Selection.SpecialistID = "beth"
	three, _ := bookMassage(t, f, other, args)
	// Stored labels are untrusted; JSON escaping must not exceed the advertised result limit.
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_bookings SET snapshot=jsonb_set(snapshot,'{event_name}',to_jsonb($2::text)) WHERE event_id=$1", f.Event.ID, strings.Repeat("<", 4500))
	require.NoError(t, err)
	page := readAdministrativeMassage(t, f, uuid.Nil, 20)
	require.NotEmpty(t, page.Bookings)
	require.Less(t, len(page.Bookings), 3)
	require.NotNil(t, page.NextAfter)
	found := map[uuid.UUID]bool{}
	for count := 0; ; count++ {
		require.Less(t, count, 3, "Cursor must advance")
		for _, item := range page.Bookings {
			require.False(t, found[item.Booking.ID])
			found[item.Booking.ID] = true
		}
		if page.NextAfter == nil {
			break
		}
		page = readAdministrativeMassage(t, f, *page.NextAfter, 20)
	}
	require.Equal(t, map[uuid.UUID]bool{one.ID: true, two.ID: true, three.ID: true}, found)
}
