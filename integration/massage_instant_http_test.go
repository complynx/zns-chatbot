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

const instantAgentPath = "/internal/agent/massage_staff/instant"

func instantHTTPFixture(
	t *testing.T,
) (paymentFixture, string, db.MassageInstantArguments, appSession) {
	t.Helper()
	f, _, token, schedule, session := staffHTTPFixture(t)
	args := moveMassagePartyToNow(t, &f, ownerProfileScope(t, f.H, f.AdminID), schedule)
	return f, token, args, session
}

type instantHTTPPreparation struct {
	Status        string                     `json:"status"`
	Arguments     db.MassageInstantArguments `json:"arguments"`
	Description   string                     `json:"description"`
	ExpiresAt     time.Time                  `json:"expires_at"`
	Preconditions struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
}

func prepareInstantHTTP(
	t *testing.T,
	f paymentFixture,
	token string,
	operation uuid.UUID,
	args any,
) (instantHTTPPreparation, map[string]any) {
	t.Helper()
	code, raw := adminRequest(t, f.H, token, http.MethodPost, instantAgentPath+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args})
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared instantHTTPPreparation
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.Equal(t, "prepared", prepared.Status)
	require.NotEqual(t, uuid.Nil, prepared.Preconditions.ProposalID)
	return prepared, map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        prepared.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": prepared.Preconditions.ProposalID},
		},
	}
}

func TestMassageInstantHTTPBothHandoffsPreserveFrozenProposalAndOriginalStatus(t *testing.T) {
	t.Parallel()
	f, token, args, session := instantHTTPFixture(t)
	path := "/app/massage-staff/" + args.EventID.String() + "/instant"
	operation := uuid.New()
	prepared, apply := prepareInstantHTTP(t, f, token, operation, args)
	nativeOperation := uuid.New()
	nativeBody := map[string]any{
		"operation_id": nativeOperation,
		"arguments":    args,
		"proposal_id":  prepared.Preconditions.ProposalID,
	}
	native := massageAppCall[db.MassageInstantPreparation](t, f, session, http.MethodPost, path+"/prepare", nativeBody)
	require.Equal(t, prepared.Preconditions.ProposalID, native.ID)
	require.Equal(t, prepared.ExpiresAt, native.ExpiresAt)
	require.Contains(t, prepared.Description, native.Snapshot.Terms.StartsAt.Format(time.RFC3339))
	require.Contains(t, prepared.Description, native.Snapshot.Terms.Timezone)
	require.Contains(t, prepared.Description, "Reserve your own specialist time?")
	nativeBody["confirmed"] = true
	booked := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path, nativeBody)
	assertMassageWireEqual(t, native.Snapshot, booked.Snapshot)
	require.Equal(t, db.MassageSourceMiniApp, booked.Source)
	code, raw := adminRequest(t, f.H, token, http.MethodPost, instantAgentPath, apply)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, booked, massageAgentResult(t, raw))
	code, raw = adminRequest(t, f.H, token, http.MethodPost, instantAgentPath+"/status",
		map[string]any{"protocol_version": 1, "operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, booked, massageAgentResult(t, raw))
	code, raw = adminRequest(t, f.H, token, http.MethodPost, massageOwnerAPI+"book/status",
		map[string]any{"protocol_version": 1, "operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	// Explicit owner cancellation releases the slot before the reverse handoff.
	cancelPath := massageAppPath(args.EventID) + "/cancel"
	cancelBody := map[string]any{"operation_id": uuid.New(), "arguments": db.MassageCancellationArguments{
		EventID: args.EventID, BookingID: booked.ID, ExpectedVersion: booked.Version}}
	cp := massageAppCall[db.MassageCancellationPreparation](
		t,
		f,
		session,
		http.MethodPost,
		cancelPath+"/prepare",
		cancelBody,
	)
	cancelBody["proposal_id"], cancelBody["confirmed"] = cp.ID, true
	cancelled := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, cancelPath, cancelBody)
	require.Equal(t, "cancelled", cancelled.State)
	newNativeOperation := uuid.New()
	newBody := map[string]any{"operation_id": newNativeOperation, "arguments": args}
	newNative := massageAppCall[db.MassageInstantPreparation](t, f, session, http.MethodPost, path+"/prepare", newBody)
	require.NotEqual(t, native.ID, newNative.ID)
	var adopt map[string]any
	require.NoError(t, json.Unmarshal([]byte(paymentAppJSON(t, args)), &adopt))
	adopt["existing_proposal_id"] = newNative.ID
	agentOperation := uuid.New()
	adopted, agentApply := prepareInstantHTTP(t, f, token, agentOperation, adopt)
	require.Equal(t, newNative.ID, adopted.Preconditions.ProposalID)
	require.Equal(t, newNative.ExpiresAt, adopted.ExpiresAt)
	code, raw = adminRequest(t, f.H, token, http.MethodPost, instantAgentPath, agentApply)
	require.Equal(t, http.StatusOK, code, string(raw))
	agentBooking := massageAgentResult(t, raw)
	require.Equal(t, db.MassageSourceAgent, agentBooking.Source)
	newBody["proposal_id"], newBody["confirmed"] = newNative.ID, true
	nativeReplay := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path, newBody)
	require.Equal(t, agentBooking, nativeReplay)
	receipt := massageAppCall[db.MassageOperationReceipt](t, f, session, http.MethodGet,
		massageAppPath(args.EventID)+"/operations/"+newNativeOperation.String(), nil)
	require.Equal(t, "instant", receipt.Kind)
	require.Equal(t, db.MassageSourceMiniApp, receipt.Source)
	require.Equal(t, agentBooking, receipt.Result)
}

func TestMassageInstantHTTPNativeOnlyAndCallerBoundary(t *testing.T) {
	t.Parallel()
	f, token, args, session := instantHTTPFixture(t)
	path := "/app/massage-staff/" + args.EventID.String() + "/instant"
	for _, denied := range []string{f.Owner, f.Admin} {
		code, raw := adminRequest(t, f.H, denied, http.MethodPost, instantAgentPath+"/prepare",
			map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": args})
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	var injected map[string]any
	require.NoError(t, json.Unmarshal([]byte(paymentAppJSON(t, args)), &injected))
	selection, ok := injected["selection"].(map[string]any)
	require.True(t, ok)
	selection["starts_at"] = "2026-10-25T02:00:00+01:00"
	code, raw := adminRequest(t, f.H, token, http.MethodPost, instantAgentPath+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": injected})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/prepare", paymentAppJSON(t,
		map[string]any{"operation_id": uuid.New(), "arguments": injected}), session.Token, appTestOrigin)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body := map[string]any{"operation_id": uuid.New(), "arguments": args}
	prep := massageAppCall[db.MassageInstantPreparation](t, f, session, http.MethodPost, path+"/prepare", body)
	body["proposal_id"] = prep.ID
	code, raw, _ = appCall(t, f.H, http.MethodPost, path, paymentAppJSON(t, body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmed"] = true
	booked := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path, body)
	require.Equal(t, "booked", booked.State)
	replay := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost, path, body)
	require.Equal(t, booked, replay)
	var count, tasks int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_bookings").Scan(&count),
	)
	require.Equal(t, 1, count)
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Zero(t, tasks)
	f.H.peers.mu.Lock()
	defer f.H.peers.mu.Unlock()
	require.Empty(t, f.H.peers.runs, "Native confirmation does not launch Loom")
}
