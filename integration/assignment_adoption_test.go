package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func adoptAssignmentHTTP(t *testing.T, h *harness, token, operation string, original assignmentProposal) (int, []byte) {
	t.Helper()
	return adminRequest(t, h, token, http.MethodPost, "/internal/agent/administration/assign/prepare", map[string]any{
		"protocol_version": 1, "operation_id": operation, "arguments": map[string]any{
			"event_id":               original.Arguments.EventID,
			"registration_id":        original.Arguments.RegistrationID,
			"expected_event_version": original.Arguments.ExpectedEventVersion,
			"expected_version":       original.Arguments.ExpectedVersion,
			"resume_proposal_id":     original.Preconditions.ProposalID,
		},
	})
}

func inspectAssignmentProposal(t *testing.T, h *harness, token string, id uuid.UUID) db.AssignmentProposalView {
	t.Helper()
	code, raw := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/administration/assignment_proposal",
		map[string]any{
			"protocol_version": 1, "operation_id": "inspect", "arguments": map[string]any{"proposal_id": id},
		},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var result struct {
		Result db.AssignmentProposalView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	return result.Result
}

func TestAssignmentAdoptionSharesCanonicalReceiptAcrossRestartAndConcurrentEnvelopes(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	adminID, operator := registrationUser(t, h, 98501, true)
	_, owner := registrationUser(t, h, 98502, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, owner, event.ID, "join", "leader")
	original := prepareAssignment(t, h, operator, "native-original", event, entry)
	for _, operation := range []string{"loom-envelope", "second-envelope"} {
		code, raw := adoptAssignmentHTTP(t, h, operator, operation, original)
		require.Equal(t, http.StatusOK, code, string(raw))
		var adopted assignmentProposal
		require.NoError(t, json.Unmarshal(raw, &adopted))
		require.Equal(t, operation, adopted.OperationID)
		require.Equal(t, original.Preconditions, adopted.Preconditions)
		require.Equal(t, original.Arguments, adopted.Arguments)
		require.Equal(t, original.Description, adopted.Description)
		require.True(t, original.ExpiresAt.Equal(adopted.ExpiresAt))
	}
	pending := inspectAssignmentProposal(t, h, operator, original.Preconditions.ProposalID)
	require.Equal(t, db.AssignmentProposalPending, pending.State)
	require.Equal(t, original.OperationID, pending.OriginalOperationID)
	require.Nil(t, pending.Result)
	admin, err := h.store.GetUser(t.Context(), adminID)
	require.NoError(t, err)
	scope := db.EventScope{BotID: 123, Organization: "org"}
	h.stop(t)
	h.start(t)
	reopened, err := db.Open(t.Context(), h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	type outcome struct {
		result registrations.Registration
		err    error
	}
	finished := make(chan outcome, 2)
	for index, operation := range []string{original.OperationID, "loom-envelope"} {
		store := h.store
		if index == 1 {
			store = reopened
		}
		go func() {
			r, e := store.ConfirmAssignment(
				t.Context(),
				scope,
				admin.Subject,
				operation,
				original.Arguments,
				original.Preconditions.ProposalID,
			)
			finished <- outcome{r, e}
		}()
	}
	var assigned registrations.Registration
	for range 2 {
		select {
		case result := <-finished:
			require.NoError(t, result.err)
			require.Equal(t, "assigned", result.result.State)
			require.NotNil(t, result.result.Pass)
			if assigned.Pass == nil {
				assigned = result.result
			} else {
				require.Equal(t, assigned, result.result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent assignment did not finish")
		}
	}
	for _, operation := range []string{original.OperationID, "loom-envelope", "second-envelope"} {
		receipt, e := reopened.AssignmentOperation(t.Context(), scope, admin.Subject, operation)
		require.NoError(t, e)
		require.Equal(t, assigned, receipt)
		code, raw := adminRequest(
			t,
			h,
			operator,
			http.MethodPost,
			"/internal/agent/administration/assign/status",
			map[string]any{"protocol_version": 1, "operation_id": operation},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, assigned, assignmentResult(t, raw))
		var envelope struct {
			OperationID string `json:"operation_id"`
		}
		require.NoError(t, json.Unmarshal(raw, &envelope))
		require.Equal(t, operation, envelope.OperationID)
	}
	var passes, receipts int
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Equal(t, 1, passes)
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registration_operations WHERE actor=$1", admin.Subject).
			Scan(&receipts),
	)
	require.Equal(t, 1, receipts)
	_, err = h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_assignment_preparations SET expires_at=clock_timestamp()-" +
"interval '1 minute' WHERE proposal_id=$1", original.Preconditions.ProposalID)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules='{}',version=version+1 WHERE id=$1", event.ID)
	require.NoError(t, err)
	receipt, err := reopened.ConfirmAssignment(
		t.Context(),
		scope,
		admin.Subject,
		"second-envelope",
		original.Arguments,
		original.Preconditions.ProposalID,
	)
	require.NoError(t, err)
	require.Equal(t, assigned, receipt)
	completed := inspectAssignmentProposal(t, h, operator, original.Preconditions.ProposalID)
	require.Equal(t, db.AssignmentProposalCompleted, completed.State)
	require.Equal(t, assigned, *completed.Result)
	code, raw := adoptAssignmentHTTP(t, h, operator, "new-after-completion", original)
	require.Equal(t, http.StatusConflict, code, string(raw))
	changed := original.Arguments
	changed.ExpectedVersion++
	_, err = reopened.ConfirmAssignment(
		t.Context(),
		scope,
		admin.Subject,
		"loom-envelope",
		changed,
		original.Preconditions.ProposalID,
	)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestAssignmentAdoptionRejectsForeignChangedAndOccupiedTransportKeys(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	adminID, operator := registrationUser(t, h, 98601, true)
	otherID, other := registrationUser(t, h, 98602, true)
	_, owner := registrationUser(t, h, 98603, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, owner, event.ID, "join", "leader")
	original := prepareAssignment(t, h, operator, "original", event, entry)
	code, raw := adoptAssignmentHTTP(t, h, other, "foreign", original)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, _ = adminRequest(
		t,
		h,
		owner,
		http.MethodPost,
		"/internal/agent/administration/assignment_proposals",
		map[string]any{"protocol_version": 1, "operation_id": "read", "arguments": map[string]any{}},
	)
	require.Equal(t, http.StatusForbidden, code)
	code, _ = adminRequest(
		t,
		h,
		other,
		http.MethodPost,
		"/internal/agent/administration/assignment_proposal",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "read",
			"arguments":        map[string]any{"proposal_id": original.Preconditions.ProposalID},
		},
	)
	require.Equal(t, http.StatusNotFound, code)
	changed := original
	changed.Arguments.ExpectedVersion++
	code, raw = adoptAssignmentHTTP(t, h, operator, "changed", changed)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw = adoptAssignmentHTTP(t, h, operator, "alias", original)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw = adoptAssignmentHTTP(t, h, operator, "alias", original)
	require.Equal(t, http.StatusOK, code, string(raw))
	var replay assignmentProposal
	require.NoError(t, json.Unmarshal(raw, &replay))
	require.Equal(t, original.Preconditions, replay.Preconditions)
	require.True(t, original.ExpiresAt.Equal(replay.ExpiresAt))
	admin, err := h.store.GetUser(t.Context(), adminID)
	require.NoError(t, err)
	scope := db.EventScope{BotID: 123, Organization: "org"}
	// A canonical preparation also reserves its ID against an unrelated owner join.
	_, err = h.store.ChangeRegistration(
		t.Context(),
		scope,
		admin.Subject,
		original.OperationID,
		db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: adminID, Role: "leader"},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	stillPending := inspectAssignmentProposal(t, h, operator, original.Preconditions.ProposalID)
	require.Equal(t, db.AssignmentProposalPending, stillPending.State)
	// A reserved alias cannot become a separate participant or unprepared operator operation.
	_, err = h.store.ChangeRegistration(
		t.Context(),
		scope,
		admin.Subject,
		"alias",
		db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: adminID, Role: "leader"},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.ChangeRegistration(
		t.Context(),
		scope,
		admin.Subject,
		"occupied",
		db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: adminID, Role: "leader"},
	)
	require.NoError(t, err)
	code, raw = adoptAssignmentHTTP(t, h, operator, "occupied", original)
	require.Equal(t, http.StatusConflict, code, string(raw))
	// An existing distinct canonical key cannot be rebound to this proposal either.
	otherEntry := registerForEvent(t, h, other, event.ID, "other-join", "follower")
	otherProposal := prepareAssignment(t, h, operator, "other-preparation", event, otherEntry)
	code, raw = adoptAssignmentHTTP(t, h, operator, "other-preparation", original)
	require.Equal(t, http.StatusConflict, code, string(raw))
	page, err := h.store.PendingAssignmentProposals(t.Context(), scope, admin.Subject, uuid.Nil, 1)
	require.NoError(t, err)
	require.Len(t, page.Proposals, 1)
	require.NotNil(t, page.Next)
	next, err := h.store.PendingAssignmentProposals(t.Context(), scope, admin.Subject, *page.Next, 1)
	require.NoError(t, err)
	require.Len(t, next.Proposals, 1)
	require.NotEqual(t, page.Proposals[0].Preparation.ID, next.Proposals[0].Preparation.ID)
	require.Nil(t, next.Next)
	foreign, err := h.store.GetUser(t.Context(), otherID)
	require.NoError(t, err)
	empty, err := h.store.PendingAssignmentProposals(t.Context(), scope, foreign.Subject, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, empty.Proposals)
	// Resume locator cannot substitute a different exact-confirmation proposal.
	body := assignmentExecutionBody(otherProposal)
	body["arguments"] = map[string]any{
		"event_id":               event.ID,
		"registration_id":        otherEntry.ID,
		"expected_version":       otherEntry.Version,
		"expected_event_version": event.Version,
		"resume_proposal_id":     original.Preconditions.ProposalID,
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, "/internal/agent/administration/assign", body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	var passes int
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestAssignmentAdoptionRechecksExpiryAfterCanonicalLock(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	adminID, operator := registrationUser(t, h, 98701, true)
	_, owner := registrationUser(t, h, 98702, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, owner, event.ID, "join", "leader")
	proposal := prepareAssignment(t, h, operator, "original", event, entry)
	admin, err := h.store.GetUser(t.Context(), adminID)
	require.NoError(t, err)
	writer, err := h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(t.Context()) }()
	_, err = writer.Exec(
		t.Context(),
		"SELECT proposal_id FROM shuttle_assignment_preparations WHERE proposal_id=$1 FOR UPDATE",
		proposal.Preconditions.ProposalID,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, e := h.store.AdoptAssignment(
			ctx,
			db.EventScope{BotID: 123, Organization: "org"},
			admin.Subject,
			"after-wait",
			proposal.Arguments,
			proposal.Preconditions.ProposalID,
		)
		finished <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := h.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_bl" +
"ocking_pids(pid)))", writer.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	_, err = writer.Exec(
		ctx,
		"UPDATE shuttle_assignment_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1",
		proposal.Preconditions.ProposalID,
	)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))
	select {
	case err = <-finished:
		require.ErrorIs(t, err, db.ErrConflict)
	case <-ctx.Done():
		t.Fatal("adoption did not finish")
	}
	view := inspectAssignmentProposal(t, h, operator, proposal.Preconditions.ProposalID)
	require.Equal(t, db.AssignmentProposalExpired, view.State)
	require.Nil(t, view.Result)
	var aliases int
	require.NoError(t, h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_assignment_aliases").Scan(&aliases))
	require.Zero(t, aliases)
}
