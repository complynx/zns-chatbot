package integration_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPassSalesRequireCodesAndPreserveDirectedInvitations(t *testing.T) {
	t.Parallel()
	for name, policy := range map[string]string{
		"automatic": `{"auto_assign":true}`,
		"scheduled": `{"sales_opens_at":"2020-01-01T00:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			f := setupPair(t)
			prior := invitePair(t, f)
			_, err := f.H.store.Pool().Exec(ctx,
				"UPDATE shuttle_event_registration_rules SET document=document || $2::jsonb WHERE event_id=$1",
				f.EventID, policy)
			require.NoError(t, err)
			preview, err := f.H.store.PairInvitationContext(ctx, f.Other, f.EventID)
			require.NoError(t, err)
			require.Equal(t, "/passes "+f.EventID.String()+" --pair", preview.PassCommand)
			args := db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version}
			replay, err := f.H.store.CreatePairInvitation(ctx, f.A, "pair-invite", args)
			require.NoError(t, err)
			require.Equal(t, prior, replay)
			closed, err := f.H.store.DecidePairInvitation(ctx, f.B, "close-old", "decline", db.PairInvitationDecision{
				EventID: f.EventID, InvitationID: prior.Invitation.ID, ExpectedVersion: prior.Invitation.Version,
			})
			require.NoError(t, err)
			require.Equal(t, "declined", closed.Invitation.State)
			entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
			require.NoError(t, err)
			args.ExpectedVersion = entry.Version
			_, err = f.H.store.CreatePairInvitation(ctx, f.A, "new-directed", args)
			require.ErrorIs(t, err, registrations.ErrUnavailable)
			code, err := f.H.store.IssuePassInvite(ctx, f.A, f.EventID, "issue-code", uuid.Nil)
			require.NoError(t, err)
			require.NotEmpty(t, code.Code)
		})
	}
}

// The first claimant can finish their details later. A competing task cannot
// consume the code or create a partial registration while that claim is open.
func TestPassInviteFirstClaimSurvivesUntilRegistration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := setupPair(t)
	invite, err := f.H.store.IssuePassInvite(ctx, f.A, f.EventID, "issue", uuid.Nil)
	require.NoError(t, err)
	_, err = f.H.store.ClaimPassInvite(ctx, f.B, f.EventID, invite.Code, registrations.Leader)
	require.ErrorIs(t, err, db.ErrConflict)
	type outcome struct {
		owner db.PaymentScope
		err   error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, owner := range []db.PaymentScope{f.B, f.Other} {
		wg.Go(func() {
			_, claimErr := f.H.store.ClaimPassInvite(ctx, owner, f.EventID, invite.Code)
			results <- outcome{owner, claimErr}
		})
	}
	wg.Wait()
	close(results)
	var winner db.PaymentScope
	count := 0
	for result := range results {
		if result.err == nil {
			winner = result.owner
			count++
		} else {
			require.ErrorIs(t, result.err, db.ErrConflict)
		}
	}
	require.Equal(t, 1, count)
	var registrationsBefore int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_registrations WHERE event_id=$1", f.EventID).
			Scan(&registrationsBefore),
	)
	require.Equal(t, 1, registrationsBefore)
	_, err = f.H.store.ChangeRegistration(
		ctx,
		winner.EventScope,
		winner.Subject,
		"complete",
		db.RegistrationChange{Kind: "register", UserID: winner.UserID, EventID: f.EventID, Role: registrations.Follower},
	)
	require.NoError(t, err)
	result, err := f.H.store.ClaimPassInvite(ctx, winner, f.EventID, invite.Code)
	require.NoError(t, err)
	require.NotNil(t, result.PairID)
	var members int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_active_pair_members WHERE pair_id=$1", *result.PairID).
			Scan(&members),
	)
	require.Equal(t, 2, members)
}

func TestPassInviteReplacementCommitsOnEligibleClaim(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	store := f.H.store
	code, err := store.IssuePassInvite(ctx, f.A, f.EventID, "replace", args.PairID)
	require.NoError(t, err)
	_, err = store.ClaimPassInvite(ctx, f.Other, f.EventID, code.Code)
	require.NoError(t, err)
	// Old partners stay linked until the new claimant has a registration.
	var state string
	require.NoError(t, store.Pool().QueryRow(ctx, "SELECT state FROM shuttle_pairs WHERE id=$1", args.PairID).Scan(&state))
	require.Equal(t, registrations.Waitlisted, state)
	_, err = store.PreparePairAssignment(ctx, admin, "premature", args)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = store.ChangeRegistration(
		ctx,
		f.Other.EventScope,
		f.Other.Subject,
		"claimant-ready",
		db.RegistrationChange{Kind: "register", UserID: f.Other.UserID, EventID: f.EventID, Role: registrations.Follower},
	)
	require.NoError(t, err)
	completed, err := store.ClaimPassInvite(ctx, f.Other, f.EventID, code.Code)
	require.NoError(t, err)
	require.NotNil(t, completed.PairID)
	require.NotEqual(t, args.PairID, *completed.PairID)
	require.NoError(t, store.Pool().QueryRow(ctx, "SELECT state FROM shuttle_pairs WHERE id=$1", args.PairID).Scan(&state))
	require.Equal(t, "dissolved", state)
	for _, owner := range []db.PaymentScope{f.A, f.B, f.Other} {
		entry, readErr := store.Registration(ctx, owner.EventScope, f.EventID, owner.UserID)
		require.NoError(t, readErr)
		require.Equal(t, registrations.Waitlisted, entry.State)
		require.Nil(t, entry.Pass)
	}
}

func TestAssignedOwnerCannotIssueOrConsumePassInvite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := setupPair(t)
	adminID, _ := registrationUser(t, f.H, 12005, true)
	admin := paymentScope(t, f.H, adminID, true)
	store := f.H.store
	_, err := store.ChangeRegistration(ctx, admin.EventScope, admin.Subject, "solo-assign", db.RegistrationChange{
		Kind:                 "assign",
		EventID:              f.EventID,
		RegistrationID:       f.Entry.ID,
		ExpectedVersion:      f.Entry.Version,
		ExpectedEventVersion: 1,
	})
	require.NoError(t, err)
	_, err = store.IssuePassInvite(ctx, f.A, f.EventID, "ineligible", uuid.Nil)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = store.ChangeRegistration(ctx, f.B.EventScope, f.B.Subject, "participant", db.RegistrationChange{
		Kind: "register", EventID: f.EventID, UserID: f.B.UserID, Role: registrations.Follower,
	})
	require.NoError(t, err)
	code, err := store.IssuePassInvite(ctx, f.B, f.EventID, "invite", uuid.Nil)
	require.NoError(t, err)
	_, err = store.ClaimPassInvite(ctx, f.A, f.EventID, code.Code)
	require.ErrorIs(t, err, db.ErrConflict)
	next, err := store.ClaimPassInvite(ctx, f.Other, f.EventID, code.Code)
	require.NoError(t, err)
	require.Nil(t, next.PairID)
}

func TestPassInviteHTTPAndOperatorDissolution(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	h := f.H
	token := func(s db.PaymentScope) string {
		h.peers.mu.Lock()
		defer h.peers.mu.Unlock()
		return h.peers.token(s.Subject)
	}
	ctx := t.Context()
	_, err := h.store.ChangeRegistration(ctx, f.B.EventScope, f.B.Subject, "waiting", db.RegistrationChange{
		Kind: "register", EventID: f.EventID, UserID: f.B.UserID, Role: registrations.Follower,
	})
	require.NoError(t, err)
	path := "/internal/events/" + f.EventID.String() + "/pass-invites"
	status, body := adminRequest(t, h, token(f.A), http.MethodPost, path, map[string]any{"operation_id": "http-issue"})
	require.Equal(t, http.StatusOK, status, string(body))
	var invite db.PassInvite
	require.NoError(t, json.Unmarshal(body, &invite))
	require.NotEmpty(t, invite.Code)
	status, body = adminRequest(t, h, token(f.B), http.MethodPost, path+"/claim", map[string]any{"code": invite.Code})
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &invite))
	require.NotNil(t, invite.PairID)
	leave := "/internal/admin/events/" + f.EventID.String() + "/pairs/" + invite.PairID.String() + "/dissolve"
	args := map[string]any{"operation_id": "admin-dissolve", "expected_version": 1}
	status, _ = adminRequest(t, h, token(f.Other), http.MethodPost, leave, args)
	require.Equal(t, http.StatusForbidden, status)
	_, adminToken := registrationUser(t, h, 12005, true)
	status, body = adminRequest(t, h, adminToken, http.MethodPost, leave, args)
	require.Equal(t, http.StatusOK, status, string(body))
	status, replay := adminRequest(t, h, adminToken, http.MethodPost, leave, args)
	require.Equal(t, http.StatusOK, status, string(replay))
	var first, again map[string]any
	require.NoError(t, json.Unmarshal(body, &first))
	require.NoError(t, json.Unmarshal(replay, &again))
	delete(first, "$schema")
	delete(again, "$schema")
	require.Equal(t, first, again)
	_, err = h.store.ClaimPassInvite(ctx, f.B, f.EventID, invite.Code)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.IssuePassInvite(ctx, f.A, f.EventID, "http-issue", uuid.Nil)
	require.ErrorIs(t, err, db.ErrConflict)
}
