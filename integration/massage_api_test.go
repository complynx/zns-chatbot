package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const massageWritePath = "/internal/agent/administration/set_massage_schedule"

func prepareMassage(
	t *testing.T,
	f paymentFixture,
	op uuid.UUID,
	a db.MassageScheduleChange,
) db.MassageSchedulePreparation {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, massageWritePath+"/prepare", map[string]any{
		"protocol_version": 1, "operation_id": op, "arguments": a})
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Arguments     db.MassageScheduleChange `json:"arguments"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	return db.MassageSchedulePreparation{ID: response.Preconditions.ProposalID, Arguments: response.Arguments}
}

func commitMassage(
	t *testing.T,
	f paymentFixture,
	token string,
	op uuid.UUID,
	p db.MassageSchedulePreparation,
) (int, string, db.MassageSchedule) {
	t.Helper()
	code, raw := adminRequest(t, f.H, token, http.MethodPost, massageWritePath, map[string]any{
		"protocol_version": 1,
		"operation_id":     op,
		"idempotency_key":  op,
		"arguments":        p.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": p.ID},
		},
	})
	var response struct {
		Status string             `json:"status"`
		Result db.MassageSchedule `json:"result"`
	}
	if code == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &response))
	}
	return code, response.Status, response.Result
}

func massageRead(t *testing.T, f paymentFixture, token, method string, args any) (int, []byte) {
	t.Helper()
	return adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/massage/"+method,
		map[string]any{"protocol_version": 1, "operation_id": "massage-read", "arguments": args})
}

func TestMassageHTTPExactConfigurationAndOwnerCandidatePages(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	a := db.MassageScheduleChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		Document: massageDocument(f.Event.Document.StartsAt)}
	op := uuid.New()
	p := prepareMassage(t, f, op, a)
	code, _, _ := commitMassage(t, f, f.Owner, op, p)
	require.Equal(t, http.StatusForbidden, code, "Owner cannot apply an administrative proposal")
	code, status, first := commitMassage(t, f, f.Admin, op, p)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "succeeded", status)
	code, _ = massageRead(t, f, f.Owner, "schedule", map[string]any{"event_id": f.Event.ID})
	require.Equal(t, http.StatusNotFound, code)
	f.Event.Document.Modules = append(f.Event.Document.Modules, "massage")
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPut,
		"/internal/admin/events/"+f.Event.ID.String(),
		map[string]any{
			"operation_id":     "enable-massage",
			"expected_version": f.Event.Version,
			"document":         f.Event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &f.Event))
	code, raw = massageRead(t, f, f.Owner, "schedule", map[string]any{"event_id": f.Event.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var schedule struct {
		Result db.MassageScheduleView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &schedule))
	require.True(t, schedule.Result.ScheduleOnly)
	require.False(t, schedule.Result.ReservationSupported)
	require.Equal(t, first.Version, schedule.Result.Version)
	args := db.MassageCandidateArguments{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      first.Version,
	}
	args.Query.WindowID, args.Query.SpecialistID, args.Query.ServiceID = "night", "alex", "short"
	args.Query.From, args.Query.Until, args.Query.Limit = a.Document.Windows[0].StartsAt, a.Document.Windows[0].EndsAt, 2
	code, raw = massageRead(t, f, f.Owner, "candidate_starts", args)
	require.Equal(t, http.StatusOK, code, string(raw))
	var page struct {
		Result db.MassageCandidateView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Result.Candidates, 2)
	require.True(t, page.Result.HasMore)
	require.Equal(t, 1200, page.Result.OccupancySeconds)
	require.Equal(t, 900, page.Result.TreatmentSeconds)
	previous := page.Result.Candidates[1].StartsAt
	args.Query.AfterStart = page.Result.NextAfter
	code, raw = massageRead(t, f, f.Owner, "candidate_starts", args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page))
	require.True(t, page.Result.Candidates[0].StartsAt.After(previous))
	args.ExpectedVersion++
	code, _ = massageRead(t, f, f.Owner, "candidate_starts", args)
	require.Equal(t, http.StatusConflict, code)
	for _, token := range []string{f.Admin, f.Ambassador} {
		code, _ = massageRead(t, f, token, "schedule", map[string]any{"event_id": f.Event.ID})
		require.Equal(t, http.StatusNotFound, code, "Administrative roles alone do not grant owner entitlement")
	}
	code, status, replay := commitMassage(t, f, f.Admin, op, p)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "succeeded", status)
	require.Equal(t, first, replay, "Successful retry survives later event version changes")
	_, err := f.H.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	code, _ = massageRead(t, f, f.Owner, "schedule", map[string]any{"event_id": f.Event.ID})
	require.Equal(t, http.StatusForbidden, code)
}

func TestMassageHTTPRequiresExplicitConsentFieldsAndCorrectOperation(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	a := db.MassageScheduleChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		Document: massageDocument(f.Event.Document.StartsAt)}
	op := uuid.New()
	p := prepareMassage(t, f, op, a)
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, massageWritePath, map[string]any{
		"protocol_version": 1, "operation_id": op, "idempotency_key": op, "arguments": a})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	code, status, _ := commitMassage(t, f, f.Admin, uuid.New(), p)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "rejected", status)
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, massageWritePath+"/status", map[string]any{
		"protocol_version": 1, "operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	rawArgs, err := json.Marshal(a)
	require.NoError(t, err)
	var arguments map[string]any
	require.NoError(t, json.Unmarshal(rawArgs, &arguments))
	document := arguments["document"].(map[string]any)
	delete(document["specialists"].([]any)[0].(map[string]any), "requires_table")
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, massageWritePath+"/prepare", map[string]any{
		"protocol_version": 1, "operation_id": uuid.New(), "arguments": arguments})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
}
