package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

func massageAppFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.MassageScheduleChange, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, owner, _, change := massageBookingFixtureWithPeers(t, 2, p)
	f.H.server = httptest.NewServer(f.H.endpoint.PublicHandler())
	session := detailsAppSession(t, f.H, 11002, 213)
	require.Equal(t, owner.UserID, session.UserID)
	return f, owner, change, session
}

func massageAppCall[T any](t *testing.T, f paymentFixture, session appSession, method, path string, body any) T {
	t.Helper()
	encoded := ""
	if body != nil {
		encoded = paymentAppJSON(t, body)
	}
	code, raw, headers := appCall(t, f.H, method, path, encoded, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	var result T
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func massageAppPath(event uuid.UUID) string { return "/app/massage/" + event.String() }

func TestMiniAppMassageHTTPNativeLifecycleDoesNotStartAgentWork(t *testing.T) {
	t.Parallel()
	f, _, change, session := massageAppFixture(t)
	path := massageAppPath(f.Event.ID)
	choices := massageAppCall[events.ContextList](t, f, session, http.MethodGet, "/app/massage/events", nil)
	require.Len(t, choices.Events, 1)
	require.Equal(t, f.Event.ID, choices.Events[0].ID)
	schedule := massageAppCall[db.MassageScheduleView](t, f, session, http.MethodGet, path+"/schedule", nil)
	require.Equal(t, change.ExpectedVersion, schedule.Version)
	available := massageAppCall[db.MassageAvailabilityView](
		t,
		f,
		session,
		http.MethodPost,
		path+"/availability",
		massageAvailabilityArgs(f, change, "alex", 0, 2),
	)
	require.True(t, available.Candidates[0].Available)
	operation := uuid.New()
	args := massageBookingArgs(f, change, 0)
	body := map[string]any{"operation_id": operation, "arguments": args}
	prepared := massageAppCall[db.MassageBookingPreparation](t, f, session, http.MethodPost, path+"/book/prepare", body)
	require.NotEqual(t, uuid.Nil, prepared.ID)
	body["proposal_id"], body["confirmed"] = prepared.ID, true
	booking := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path+"/book", body)
	require.Equal(t, "booked", booking.State)
	require.Equal(t, db.MassageSourceMiniApp, booking.Source)
	replay := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path+"/book", body)
	require.Equal(t, booking, replay)
	page := massageAppCall[db.MassageBookingPage](t, f, session, http.MethodGet, path+"/bookings?limit=1", nil)
	require.Len(t, page.Bookings, 1)
	require.Equal(t, booking, page.Bookings[0])
	receipt := massageAppCall[db.MassageOperationReceipt](
		t,
		f,
		session,
		http.MethodGet,
		path+"/operations/"+operation.String(),
		nil,
	)
	require.Equal(t, booking, receipt.Result)
	cancellation := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	cancelBody := map[string]any{"operation_id": uuid.New(), "arguments": cancellation}
	cancelProposal := massageAppCall[db.MassageCancellationPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/cancel/prepare",
		cancelBody,
	)
	cancelBody["proposal_id"], cancelBody["confirmed"] = cancelProposal.ID, true
	cancelled := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path+"/cancel", cancelBody)
	require.Equal(t, "cancelled", cancelled.State)
	current := massageAppCall[db.MassageBooking](
		t,
		f,
		session,
		http.MethodGet,
		path+"/bookings/"+booking.ID.String(),
		nil,
	)
	require.Equal(t, cancelled, current)
	original := massageAppCall[db.MassageOperationReceipt](
		t,
		f,
		session,
		http.MethodGet,
		path+"/operations/"+operation.String(),
		nil,
	)
	require.Equal(t, booking, original.Result, "Receipt stays original after cancellation")
	var tasks int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Zero(t, tasks, "Native business requests must not start proactive agent work")
}

func TestMiniAppMassageHTTPBothHandoffsKeepHistoryAfterPassAndModuleLoss(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	path := massageAppPath(f.Event.ID)
	operation := uuid.New()
	args := massageBookingArgs(f, change, 0)
	nativeBody := map[string]any{"operation_id": operation, "arguments": args}
	native := massageAppCall[db.MassageBookingPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/book/prepare",
		nativeBody,
	)
	proposal, body := massageAgentPreparation(t, f, f.Owner, "book", operation, args)
	require.Equal(t, native.ID, proposal)
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"book", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	booking := massageAgentResult(t, raw)
	nativeBody["proposal_id"], nativeBody["confirmed"] = native.ID, true
	recovered := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path+"/book", nativeBody)
	require.Equal(t, booking, recovered)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage'),"+
			"version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(
			t.Context(),
			"UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AND user_id=$2",
			f.Event.ID,
			owner.UserID,
		)
	require.NoError(t, err)
	cancelArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	cancelOp := uuid.New()
	cancelProposal, _ := massageAgentPreparation(t, f, f.Owner, "cancel", cancelOp, cancelArgs)
	cancelBody := map[string]any{
		"operation_id": cancelOp,
		"arguments":    cancelArgs,
		"proposal_id":  cancelProposal,
		"confirmed":    true,
	}
	cancelled := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path+"/cancel", cancelBody)
	require.Equal(t, "cancelled", cancelled.State)
	history := massageAppCall[db.MassageBookingPage](t, f, session, http.MethodGet, path+"/bookings", nil)
	require.Len(t, history.Bookings, 1)
	require.Equal(t, cancelled, history.Bookings[0])
	choices := massageAppCall[events.ContextList](t, f, session, http.MethodGet, "/app/massage/events", nil)
	require.Len(t, choices.Events, 1, "Retained own history remains discoverable")
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/schedule", "", session.Token, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/availability",
		paymentAppJSON(t, massageAvailabilityArgs(f, change, "alex", 1, 2)),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
}

func TestMiniAppMassageHTTPRejectsStaleApprovalForeignOwnerAndLoggedOutReads(t *testing.T) {
	t.Parallel()
	f, _, change, session := massageAppFixture(t)
	path := massageAppPath(f.Event.ID)
	args := massageBookingArgs(f, change, 0)
	operation := uuid.New()
	prepared := massageAppCall[db.MassageBookingPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/book/prepare",
		map[string]any{"operation_id": operation, "arguments": args},
	)
	body := map[string]any{"operation_id": operation, "arguments": args, "proposal_id": prepared.ID, "confirmed": false}
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/book",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmed"] = true
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/book",
		paymentAppJSON(t, body),
		session.Token,
		"https://foreign.example.invalid",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	changed := args
	changed.Selection.SpecialistID = "beth"
	body["arguments"] = changed
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/book", paymentAppJSON(t, body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusConflict, code, string(raw))
	body["arguments"] = args
	booking := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path+"/book", body)
	foreign := detailsAppSession(t, f.H, 11003, 214)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/bookings/"+booking.ID.String(), "", foreign.Token, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/operations/"+operation.String(), "", foreign.Token, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	for _, read := range []string{
		"/app/massage/events",
		path + "/schedule",
		path + "/bookings",
		path + "/bookings/" + booking.ID.String(),
		path + "/operations/" + operation.String(),
	} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, read, "", session.Token, "")
		require.Equal(t, http.StatusUnauthorized, code, string(raw))
	}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/availability",
		paymentAppJSON(t, massageAvailabilityArgs(f, change, "alex", 1, 2)),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}
