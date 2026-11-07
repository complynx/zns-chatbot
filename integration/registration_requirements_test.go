package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func requireEventDetails(t *testing.T, h *harness, event events.Event) events.Event {
	t.Helper()
	rules := *event.Document.Registration
	rules.DetailsRequirements = &registrationdetails.Requirements{PassportNumber: true}
	event.Document.Registration = &rules
	updated, err := h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		"requirements-fixture",
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	require.True(t, updated.Document.Registration.DetailsRequirements.LegalName)
	return updated
}

func completeRequiredDetails(t *testing.T, h *harness, owner uuid.UUID) {
	t.Helper()
	scope := ownerProfileScope(t, h, owner)
	status, err := h.store.RegistrationDetailsStatus(t.Context(), scope)
	require.NoError(t, err)
	protector := detailsProtector(t, 76)
	for _, field := range []registrationdetails.Field{registrationdetails.LegalName, registrationdetails.PassportNumber} {
		status, err = h.store.MutateRegistrationDetails(
			t.Context(),
			scope,
			registrationdetails.Mutation{
				OperationID:     uuid.New(),
				ExpectedVersion: status.Version,
				Field:           field,
				Value:           "SYNTHETIC-REGISTRATION-DETAIL",
			},
			protector,
		)
		require.NoError(t, err)
	}
}

func TestRequiredDetailsGateSoloJoinAcrossAgentAndDirectAPI(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 76)
	h := setup(t, p)
	_, admin := registrationUser(t, h, 95201, true)
	owner, token := registrationUser(t, h, 95202, false)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	scope := ownerProfileScope(t, h, owner)
	public := profileChange("set_name", 0)
	public.FullName = "A public profile name"
	changeProfile(t, h, scope, public)
	code, raw := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/events/"+event.ID.String()+"/registration",
		map[string]any{"operation_id": "missing-direct", "role": "leader"},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	operation := uuid.NewString()
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"confirmation":     map[string]any{"mode": "none"},
		"arguments":        map[string]any{"event_id": event.ID, "role": "leader"},
	}
	code, raw = adminRequest(t, h, token, http.MethodPost, "/internal/agent/registrations/join", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	_, err := h.store.Registration(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, event.ID, owner)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	code, raw = detailsRequest(
		t,
		h,
		token,
		"set_legal_name",
		uuid.New(),
		map[string]any{"expected_version": 0, "legal_name": "Synthetic Legal Name"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	user, err := h.store.GetUser(t.Context(), owner)
	require.NoError(t, err)
	_, err = h.store.ChangeRegistration(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		user.Subject,
		"still-missing",
		db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: owner, Role: "leader"},
	)
	var missing *registrationdetails.MissingError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, []registrationdetails.Field{registrationdetails.PassportNumber}, missing.MissingFields)
	code, raw = detailsRequest(
		t,
		h,
		token,
		"set_passport_number",
		uuid.New(),
		map[string]any{"expected_version": 1, "passport_number": "SYNTHETIC-PASSPORT-95202"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	h.stop(t)
	h.start(t)
	nextOperation := uuid.NewString()
	body["operation_id"], body["idempotency_key"] = nextOperation, nextOperation
	code, raw = adminRequest(t, h, token, http.MethodPost, "/internal/agent/registrations/join", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "succeeded")
	require.NotContains(t, string(raw), "SYNTHETIC-PASSPORT")
	var result struct {
		Result registrations.Registration `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, owner, result.Result.UserID)
	require.Equal(t, registrations.Waitlisted, result.Result.State)
}

func TestRequiredDetailsPreserveExistingJoinButGateAssignment(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, adminToken := registrationUser(t, h, 95211, true)
	owner, token := registrationUser(t, h, 95212, false)
	event := registrationEvent(t, h, adminToken)
	entry := registerForEvent(t, h, token, event.ID, "historical-join", "leader")
	event = requireEventDetails(t, h, event)
	require.Equal(t, entry, registerForEvent(t, h, token, event.ID, "historical-join", "leader"))
	require.Equal(t, entry, registerForEvent(t, h, token, event.ID, "existing-join", "leader"))
	scope := db.EventScope{BotID: 123, Organization: "org"}
	change := db.RegistrationChange{
		Kind:                 "assign",
		EventID:              event.ID,
		RegistrationID:       entry.ID,
		ExpectedVersion:      entry.Version,
		ExpectedEventVersion: event.Version,
	}
	_, err := h.store.ChangeRegistration(t.Context(), scope, "requirements-admin", "missing-pass", change)
	require.ErrorIs(t, err, registrationdetails.ErrRequired)
	args := db.PassAssignment{
		EventID:              event.ID,
		RegistrationID:       entry.ID,
		ExpectedVersion:      entry.Version,
		ExpectedEventVersion: event.Version,
	}
	_, err = h.store.PrepareAssignment(t.Context(), scope, "requirements-admin", "missing-preparation", args)
	require.ErrorIs(t, err, registrationdetails.ErrRequired)
	completeRequiredDetails(t, h, owner)
	prep, err := h.store.PrepareAssignment(t.Context(), scope, "requirements-admin", "assign-ready", args)
	require.NoError(t, err)
	assigned, err := h.store.ConfirmAssignment(t.Context(), scope, "requirements-admin", "assign-ready", args, prep.ID)
	require.NoError(t, err)
	require.NotNil(t, assigned.Pass)
	h.stop(t)
	h.start(t)
	replay, err := h.store.ConfirmAssignment(t.Context(), scope, "requirements-admin", "assign-ready", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, assigned, replay)
}

func TestRequiredDetailsGatePairInvitationCreatesNoRegistrationUntilReady(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	invitation := invitePair(t, f)
	event, err := f.H.store.Event(t.Context(), f.A.EventScope, f.EventID)
	require.NoError(t, err)
	requireEventDetails(t, f.H, event)
	decision := db.PairInvitationDecision{
		EventID:         f.EventID,
		InvitationID:    invitation.Invitation.ID,
		ExpectedVersion: invitation.Invitation.Version,
		Role:            registrations.Follower,
	}
	_, err = f.H.store.DecidePairInvitation(t.Context(), f.B, "missing-pair-accept", "accept", decision)
	require.ErrorIs(t, err, registrationdetails.ErrRequired)
	_, err = f.H.store.Registration(t.Context(), f.B.EventScope, f.EventID, f.B.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	completeRequiredDetails(t, f.H, f.B.UserID)
	accepted, err := f.H.store.DecidePairInvitation(t.Context(), f.B, "ready-pair-accept", "accept", decision)
	require.NoError(t, err)
	require.NotNil(t, accepted.Pair)
}

func TestRequiredDetailsGateBothPairPassesAtomically(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	event, err := f.H.store.Event(t.Context(), f.A.EventScope, f.EventID)
	require.NoError(t, err)
	event = requireEventDetails(t, f.H, event)
	args.ExpectedEventVersion = event.Version
	completeRequiredDetails(t, f.H, f.A.UserID)
	_, err = f.H.store.PreparePairAssignment(t.Context(), admin, "pair-not-ready", args)
	require.ErrorIs(t, err, registrationdetails.ErrRequired)
	for _, owner := range []uuid.UUID{f.A.UserID, f.B.UserID} {
		entry, readErr := f.H.store.Registration(t.Context(), f.A.EventScope, f.EventID, owner)
		require.NoError(t, readErr)
		require.Nil(t, entry.Pass)
	}
	completeRequiredDetails(t, f.H, f.B.UserID)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), admin, "pair-ready", args)
	require.NoError(t, err)
	// Model authoritative removal after preparation; commit must recheck both owners.
	_, err = f.H.store.Pool().Exec(t.Context(), "DELETE FROM shuttle_registration_details WHERE user_id=$1", f.B.UserID)
	require.NoError(t, err)
	_, err = f.H.store.ConfirmPairAssignment(t.Context(), admin, "pair-ready", args, prep.ID)
	require.ErrorIs(t, err, registrationdetails.ErrRequired)
	for _, owner := range []uuid.UUID{f.A.UserID, f.B.UserID} {
		entry, readErr := f.H.store.Registration(t.Context(), f.A.EventScope, f.EventID, owner)
		require.NoError(t, readErr)
		require.Nil(t, entry.Pass)
	}
	completeRequiredDetails(t, f.H, f.B.UserID)
	assigned, err := f.H.store.ConfirmPairAssignment(t.Context(), admin, "pair-ready", args, prep.ID)
	require.NoError(t, err)
	require.NotNil(t, assigned.Leader.Pass)
	require.NotNil(t, assigned.Follower.Pass)
	replay, err := f.H.store.ConfirmPairAssignment(t.Context(), admin, "pair-ready", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, assigned, replay)
}
