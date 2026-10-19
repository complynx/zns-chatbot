package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type assignmentProposal struct {
	Status        string            `json:"status"`
	OperationID   string            `json:"operation_id"`
	Arguments     db.PassAssignment `json:"arguments"`
	Preconditions struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
	Description string    `json:"description"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func prepareAssignment(
	t *testing.T,
	h *harness,
	token, operation string,
	event events.Event,
	entry registrations.Registration,
) assignmentProposal {
	t.Helper()
	code, data := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/administration/assign/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     operation,
			"arguments": db.PassAssignment{
				EventID:              event.ID,
				RegistrationID:       entry.ID,
				ExpectedEventVersion: event.Version,
				ExpectedVersion:      entry.Version,
			},
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var result assignmentProposal
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, "prepared", result.Status)
	return result
}
func executeAssignment(t *testing.T, h *harness, token string, proposal assignmentProposal) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/administration/assign",
		assignmentExecutionBody(proposal),
	)
}
func assignmentExecutionBody(proposal assignmentProposal) map[string]any {
	return map[string]any{
		"protocol_version": 1,
		"operation_id":     proposal.OperationID,
		"arguments":        proposal.Arguments,
		"idempotency_key":  proposal.OperationID,
		"confirmation":     map[string]any{"mode": "exact_parameters", "preconditions": proposal.Preconditions},
	}
}

func assignmentResult(t *testing.T, data []byte) registrations.Registration {
	t.Helper()
	var result struct {
		Status string                     `json:"status"`
		Result registrations.Registration `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, "succeeded", result.Status)
	return result.Result
}

func TestAgentAssignmentRequiresExactProposalAndReplaysAcrossRestart(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9501, true)
	_, otherAdmin := registrationUser(t, h, 9502, true)
	userID, user := registrationUser(t, h, 9503, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, user, event.ID, "join", "leader")
	proposal := prepareAssignment(t, h, operator, "assign", event, entry)
	require.Contains(t, proposal.Description, "123.45 BYN")
	require.Contains(t, proposal.Description, "9503")
	require.Contains(t, proposal.Description, "No payment is recorded")
	require.Contains(t, proposal.Description, "+03:00")
	require.LessOrEqual(t, time.Until(proposal.ExpiresAt), 5*time.Minute)
	before, err := h.store.Registration(
		context.Background(),
		db.EventScope{BotID: 123, Organization: "org"},
		event.ID,
		userID,
	)
	require.NoError(t, err)
	require.Equal(t, entry, before)
	h.stop(t)
	h.start(t)
	require.Equal(t, proposal, prepareAssignment(t, h, operator, "assign", event, entry))
	// The endpoint must reject missing or downgraded confirmation before any effect.
	code, _ := adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/assign",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     proposal.OperationID,
			"arguments":        proposal.Arguments,
			"idempotency_key":  proposal.OperationID,
		},
	)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/assign",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     proposal.OperationID,
			"arguments":        proposal.Arguments,
			"idempotency_key":  proposal.OperationID,
			"confirmation":     map[string]any{"mode": "none", "preconditions": proposal.Preconditions},
		},
	)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	forged := proposal
	forged.Preconditions.ProposalID = uuid.New()
	code, data := executeAssignment(t, h, operator, forged)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	code, data = executeAssignment(t, h, otherAdmin, proposal)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	code, _ = executeAssignment(t, h, user, proposal)
	require.Equal(t, http.StatusForbidden, code)
	forged = proposal
	forged.OperationID = "other-operation"
	code, data = executeAssignment(t, h, operator, forged)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	forged = proposal
	forged.Arguments.ExpectedEventVersion++
	code, data = executeAssignment(t, h, operator, forged)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	code, data = executeAssignment(t, h, operator, proposal)
	require.Equal(t, http.StatusOK, code, string(data))
	assigned := assignmentResult(t, data)
	require.Equal(t, userID, assigned.UserID)
	require.EqualValues(t, 12345, assigned.Pass.PriceMinor)
	// Expiry limits new effects, never reconciliation of an already committed assignment.
	_, err = h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_assignment_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", proposal.Preconditions.ProposalID)
	require.NoError(t, err)
	h.stop(t)
	h.start(t)
	code, data = executeAssignment(t, h, operator, proposal)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, assigned, assignmentResult(t, data))
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/assign/status",
		map[string]any{"protocol_version": 1, "operation_id": "assign"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, assigned, assignmentResult(t, data))
	code, data = adminRequest(
		t,
		h,
		otherAdmin,
		http.MethodPost,
		"/internal/agent/administration/assign/status",
		map[string]any{"protocol_version": 1, "operation_id": "assign"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
}

func TestAgentAssignmentRechecksPriceCapacityAndExpiry(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9601, true)
	_, aToken := registrationUser(t, h, 9602, false)
	_, bToken := registrationUser(t, h, 9603, false)
	event := registrationEvent(t, h, operator)
	a := registerForEvent(t, h, aToken, event.ID, "join", "leader")
	b := registerForEvent(t, h, bToken, event.ID, "join", "leader")
	expired := prepareAssignment(t, h, operator, "expired", event, a)
	_, err := h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_assignment_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", expired.Preconditions.ProposalID)
	require.NoError(t, err)
	code, data := executeAssignment(t, h, operator, expired)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	original := prepareAssignment(t, h, operator, "old-price", event, a)
	event.Document.Registration.PriceMinor = 20000
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{"operation_id": "price", "expected_version": event.Version, "document": event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	code, data = executeAssignment(t, h, operator, original)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	proposedA := prepareAssignment(t, h, operator, "new-price-a", event, a)
	proposedB := prepareAssignment(t, h, operator, "new-price-b", event, b)
	require.Contains(t, proposedA.Description, "200.00 BYN")
	code, data = executeAssignment(t, h, operator, proposedA)
	require.Equal(t, http.StatusOK, code, string(data))
	assigned := assignmentResult(t, data)
	require.EqualValues(t, 20000, assigned.Pass.PriceMinor)
	code, data = executeAssignment(t, h, operator, proposedB)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	code, data = adminRequest(t, h, bToken, http.MethodGet, "/internal/events/"+event.ID.String()+"/registration", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "waitlisted")
	require.NotContains(t, string(data), "price_minor")
}

func TestAgentAssignmentDeniesRevokedTargetAndAdministrator(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	adminID, operator := registrationUser(t, h, 9701, true)
	userID, user := registrationUser(t, h, 9702, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, user, event.ID, "join", "follower")
	proposal := prepareAssignment(t, h, operator, "assign", event, entry)
	_, err := h.store.SetUserRevoked(context.Background(), userID, "fixture")
	require.NoError(t, err)
	code, data := executeAssignment(t, h, operator, proposal)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	_, err = h.store.SetUserRevoked(context.Background(), adminID, "fixture")
	require.NoError(t, err)
	code, _ = executeAssignment(t, h, operator, proposal)
	require.Equal(t, http.StatusForbidden, code)
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/assign/status",
		map[string]any{"protocol_version": 1, "operation_id": "assign"},
	)
	require.Equal(t, http.StatusForbidden, code)
}

func TestAgentAssignmentExpiryCannotPassWhileWaitingForRecipient(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9801, true)
	userID, user := registrationUser(t, h, 9802, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, user, event.ID, "join", "leader")
	proposal := prepareAssignment(t, h, operator, "assign", event, entry)
	ctx := context.Background()
	writer, err := h.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(ctx) }()
	_, err = writer.Exec(ctx, "SELECT id FROM shuttle_users WHERE id=$1 FOR UPDATE", userID)
	require.NoError(t, err)
	var expiresAt time.Time
	err = h.store.Pool().
		QueryRow(ctx, "UPDATE shuttle_assignment_preparations SET expires_at=clock_timestamp()+interval '1 second' WHERE proposal_id=$1 RETURNING expires_at", proposal.Preconditions.ProposalID).
		Scan(&expiresAt)
	require.NoError(t, err)
	type response struct {
		code int
		data []byte
		err  error
	}
	finished := make(chan response, 1)
	server := httptest.NewServer(h.endpoint.InternalHandler())
	t.Cleanup(server.Close)
	body, err := json.Marshal(assignmentExecutionBody(proposal))
	require.NoError(t, err)
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		server.URL+"/internal/agent/administration/assign",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+operator)
	go func() {
		reply, callErr := server.Client().Do(request)
		if callErr != nil {
			finished <- response{err: callErr}
			return
		}
		defer reply.Body.Close()
		data, readErr := io.ReadAll(reply.Body)
		finished <- response{code: reply.StatusCode, data: data, err: readErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := h.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.wait_event_type='Lock' AND l.relation='shuttle_users'::regclass AND a.query LIKE '%FOR SHARE%')").
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var expired bool
		e := h.store.Pool().QueryRow(ctx, "SELECT clock_timestamp()>$1::timestamptz", expiresAt).Scan(&expired)
		return e == nil && expired
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit(ctx))
	select {
	case result := <-finished:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.code, string(result.data))
		require.Contains(t, string(result.data), "rejected")
	case <-time.After(3 * time.Second):
		t.Fatal("assignment did not finish after recipient lock release")
	}
	current, err := h.store.Registration(ctx, db.EventScope{BotID: 123, Organization: "org"}, event.ID, userID)
	require.NoError(t, err)
	require.Equal(t, entry, current)
}
