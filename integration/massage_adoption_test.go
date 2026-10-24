package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func massageAdoptionArguments(t *testing.T, args any, proposal uuid.UUID) map[string]any {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	result["existing_proposal_id"] = proposal
	return result
}

// This helper follows the actual Loom wire contract: use its new operation ID,
// replace input arguments with the prepared normalized arguments, and retain preconditions.
func massageAgentAdoption(t *testing.T, f paymentFixture, kind string,
	operation, proposal uuid.UUID, args any, expires time.Time,
) map[string]any {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+kind+"/prepare", map[string]any{
		"protocol_version": 1, "operation_id": operation, "arguments": massageAdoptionArguments(t, args, proposal),
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared struct {
		OperationID   uuid.UUID       `json:"operation_id"`
		Arguments     json.RawMessage `json:"arguments"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.Equal(t, operation, prepared.OperationID)
	require.Equal(t, proposal, prepared.Preconditions.ProposalID)
	require.Equal(t, expires, prepared.ExpiresAt)
	base, err := json.Marshal(args)
	require.NoError(t, err)
	require.JSONEq(
		t,
		string(base),
		string(prepared.Arguments),
		"Normalized business arguments must omit the continuation selector",
	)
	return map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        prepared.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": proposal},
		},
	}
}

func TestMassageAdoptionUsesOriginalProposalWithNewAgentAndNativeOperations(t *testing.T) {
	t.Parallel()
	f, _, change, session := massageAppFixture(t)
	path := massageAppPath(f.Event.ID)
	args := massageBookingArgs(f, change, 0)
	nativeOperation := uuid.New()
	native := massageAppCall[db.MassageBookingPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/book/prepare",
		map[string]any{"operation_id": nativeOperation, "arguments": args},
	)
	agentOperation := uuid.New()
	body := massageAgentAdoption(t, f, "book", agentOperation, native.ID, args, native.ExpiresAt)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_preparations WHERE event_id=$1", f.Event.ID).
			Scan(&count),
	)
	require.Equal(t, 1, count)
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"book", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	booking := massageAgentResult(t, raw)
	replay := massageAppCall[db.MassageBooking](
		t,
		f,
		session,
		http.MethodPost,
		path+"/book",
		map[string]any{"operation_id": nativeOperation, "arguments": args, "proposal_id": native.ID, "confirmed": true},
	)
	require.Equal(t, booking, replay)
	cancelArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	cancelOperation := uuid.New()
	cancelProposal, cancelBody := massageAgentPreparation(t, f, f.Owner, "cancel", cancelOperation, cancelArgs)
	prepared, err := f.H.store.PrepareOwnMassageCancellation(
		t.Context(),
		ownerProfileScope(t, f.H, f.OwnerID),
		cancelOperation,
		cancelArgs,
	)
	require.NoError(t, err)
	nativeCancelOperation := uuid.New()
	adopted := massageAppCall[db.MassageCancellationPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/cancel/prepare",
		map[string]any{"operation_id": nativeCancelOperation, "arguments": cancelArgs, "proposal_id": cancelProposal},
	)
	require.Equal(t, cancelProposal, adopted.ID)
	require.Equal(t, prepared.ExpiresAt, adopted.ExpiresAt)
	cancelled := massageAppCall[db.MassageBooking](
		t,
		f,
		session,
		http.MethodPost,
		path+"/cancel",
		map[string]any{
			"operation_id": nativeCancelOperation,
			"arguments":    cancelArgs,
			"proposal_id":  cancelProposal,
			"confirmed":    true,
		},
	)
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"cancel", cancelBody)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, cancelled, massageAgentResult(t, raw))
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_preparations WHERE event_id=$1", f.Event.ID).
			Scan(&count),
	)
	require.Equal(t, 2, count)
}

func TestMassageAdoptionNativeConsumptionDuringAgentApprovalIsCanonical(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	path := massageAppPath(f.Event.ID)
	args := massageBookingArgs(f, change, 0)
	nativeOperation := uuid.New()
	prepared := massageAppCall[db.MassageBookingPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/book/prepare",
		map[string]any{"operation_id": nativeOperation, "arguments": args},
	)
	agentOperation := uuid.New()
	body := massageAgentAdoption(t, f, "book", agentOperation, prepared.ID, args, prepared.ExpiresAt)
	native := massageAppCall[db.MassageBooking](
		t,
		f,
		session,
		http.MethodPost,
		path+"/book",
		map[string]any{
			"operation_id": nativeOperation,
			"arguments":    args,
			"proposal_id":  prepared.ID,
			"confirmed":    true,
		},
	)
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"book", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, native, massageAgentResult(t, raw))
	cancelMassageForTest(t, f, owner, native)
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"book", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, native, massageAgentResult(t, raw), "Original receipt survives later cancellation")
	_, err := f.H.store.AdoptOwnMassageBooking(t.Context(), owner, uuid.New(), prepared.ID, args)
	require.ErrorIs(t, err, db.ErrConflict, "Consumed proposal is recovered as history, not newly approved")
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_bookings WHERE event_id=$1", f.Event.ID).
			Scan(&count),
	)
	require.Equal(t, 1, count)
}

func TestMassageAdoptionRejectsWrongBindingExpiryAndPrepareConsent(t *testing.T) {
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
	wrong := args
	wrong.Selection.SpecialistID = "beth"
	cases := []struct {
		name, token string
		proposal    uuid.UUID
		args        db.MassageBookingArguments
		code        int
	}{
		{"foreign owner", f.Ambassador, prepared.ID, args, http.StatusNotFound},
		{"admin is not owner", f.Admin, prepared.ID, args, http.StatusNotFound},
		{"unknown proposal", f.Owner, uuid.New(), args, http.StatusNotFound},
		{"explicit zero proposal", f.Owner, uuid.Nil, args, http.StatusUnprocessableEntity},
		{"changed selection", f.Owner, prepared.ID, wrong, http.StatusConflict},
	}
	for _, tc := range cases {
		code, raw := adminRequest(
			t,
			f.H,
			tc.token,
			http.MethodPost,
			massageOwnerAPI+"book/prepare",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     uuid.New(),
				"arguments":        massageAdoptionArguments(t, tc.args, tc.proposal),
			},
		)
		require.Equal(t, tc.code, code, "%s: %s", tc.name, raw)
	}
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/book/prepare",
		paymentAppJSON(
			t,
			map[string]any{
				"operation_id": uuid.New(),
				"arguments":    args,
				"proposal_id":  prepared.ID,
				"confirmed":    true,
			},
		),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_preparations SET expires_at=clock_timestamp()-int" +
"erval '1 second' WHERE id=$1", prepared.ID)
	require.NoError(t, err)
	code, raw = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		massageOwnerAPI+"book/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"arguments":        massageAdoptionArguments(t, args, prepared.ID),
		},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/book/prepare",
		paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "arguments": args, "proposal_id": prepared.ID}),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_preparations WHERE event_id=$1", f.Event.ID).
			Scan(&count),
	)
	require.Equal(t, 1, count)
}

func TestMassageAdoptionRejectsExplicitEmptySelectorsWithoutReplacement(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	args := massageBookingArgs(f, change, 0)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, change, 2))
	cancelArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	for _, action := range []struct {
		name string
		args any
	}{{"book", args}, {"cancel", cancelArgs}} {
		for _, selector := range []any{uuid.Nil, nil} {
			code, raw, _ := appCall(t, f.H, http.MethodPost,
				massageAppPath(f.Event.ID)+"/"+action.name+"/prepare",
				paymentAppJSON(t, map[string]any{
					"operation_id": uuid.New(), "arguments": action.args, "proposal_id": selector,
				}), session.Token, appTestOrigin)
			require.Equal(t, http.StatusBadRequest, code, string(raw))
			var agentArgs map[string]any
			require.NoError(t, json.Unmarshal([]byte(paymentAppJSON(t, action.args)), &agentArgs))
			agentArgs["existing_proposal_id"] = selector
			code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost,
				massageOwnerAPI+action.name+"/prepare", map[string]any{
					"protocol_version": 1, "operation_id": uuid.New(), "arguments": agentArgs,
				})
			require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
		}
	}
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_massage_preparations WHERE event_id=$1", f.Event.ID).Scan(&count))
	require.Equal(t, 1, count, "Invalid selectors must not create a replacement proposal")
}

func TestMassageAdoptionRechecksNewBookingAccessButKeepsHistoricalCancellation(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	path := massageAppPath(f.Event.ID)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, change, 0))
	args := massageBookingArgs(f, change, 2)
	prepared, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.NoError(t, err)
	cancelArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	cancelOp := uuid.New()
	cancelProposal, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, cancelOp, cancelArgs)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage'),versio" +
"n=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AN" +
"D user_id=$2", f.Event.ID, owner.UserID)
	require.NoError(t, err)
	code, raw := adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		massageOwnerAPI+"book/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"arguments":        massageAdoptionArguments(t, args, prepared.ID),
		},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	stale := cancelArgs
	stale.ExpectedVersion++
	code, raw = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		massageOwnerAPI+"cancel/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"arguments":        massageAdoptionArguments(t, stale, cancelProposal.ID),
		},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	agentOp := uuid.New()
	body := massageAgentAdoption(t, f, "cancel", agentOp, cancelProposal.ID, cancelArgs, cancelProposal.ExpiresAt)
	nativeOp := uuid.New()
	native := massageAppCall[db.MassageCancellationPreparation](
		t,
		f,
		session,
		http.MethodPost,
		path+"/cancel/prepare",
		map[string]any{"operation_id": nativeOp, "proposal_id": cancelProposal.ID, "arguments": cancelArgs},
	)
	require.Equal(t, cancelProposal.ID, native.ID)
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"cancel", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	cancelled := massageAgentResult(t, raw)
	recovered := massageAppCall[db.MassageBooking](
		t,
		f,
		session,
		http.MethodPost,
		path+"/cancel",
		map[string]any{
			"operation_id": nativeOp,
			"proposal_id":  cancelProposal.ID,
			"arguments":    cancelArgs,
			"confirmed":    true,
		},
	)
	require.Equal(t, cancelled, recovered)
}
