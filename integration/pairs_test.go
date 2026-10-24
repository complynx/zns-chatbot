package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type pairFixture struct {
	H           *harness
	A, B, Other db.PaymentScope
	EventID     uuid.UUID
	Entry       registrations.Registration
}

func TestPairInvitationAPIRestrictsReadsToConsentParticipants(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	invitation := invitePair(t, f)
	tokens := make(map[uuid.UUID]string)
	for _, actor := range []db.PaymentScope{f.A, f.B, f.Other} {
		f.H.peers.mu.Lock()
		tokens[actor.UserID] = f.H.peers.token(actor.Subject)
		f.H.peers.mu.Unlock()
	}
	_, administrator := registrationUser(t, f.H, 12005, true)
	request := map[string]any{
		"protocol_version": 1,
		"operation_id":     "read-invitation",
		"arguments": map[string]any{
			"event_id":      f.EventID,
			"invitation_id": invitation.Invitation.ID,
		},
	}
	for _, token := range []string{tokens[f.A.UserID], tokens[f.B.UserID]} {
		code, data := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/registrations/invitation", request)
		require.Equal(t, http.StatusOK, code, string(data))
		var response struct {
			Status string                   `json:"status"`
			Result registrations.Invitation `json:"result"`
		}
		require.NoError(t, json.Unmarshal(data, &response))
		require.Equal(t, "succeeded", response.Status)
		require.Equal(t, invitation.Invitation, response.Result)
		require.NotContains(t, string(data), "price_minor")
		require.NotContains(t, string(data), "confirmed_minor")
	}
	for _, token := range []string{tokens[f.Other.UserID], administrator} {
		code, data := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/registrations/invitation", request)
		require.Equal(t, http.StatusNotFound, code, string(data))
	}
	require.NoError(t, f.H.store.RevokeUser(context.Background(), f.B.UserID, true))
	code, data := adminRequest(
		t,
		f.H,
		tokens[f.B.UserID],
		http.MethodPost,
		"/internal/agent/registrations/invitation",
		request,
	)
	require.Equal(t, http.StatusForbidden, code, string(data))
}

func TestPairCompetingAcceptsCreateOnlyOneActiveRelationship(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	otherUser, err := f.H.store.GetUser(ctx, f.Other.UserID)
	require.NoError(t, err)
	_, err = f.H.store.ChangeRegistration(
		ctx,
		f.Other.EventScope,
		otherUser.Subject,
		"other-join",
		db.RegistrationChange{Kind: "register", EventID: f.EventID, UserID: f.Other.UserID, Role: registrations.Leader},
	)
	require.NoError(t, err)
	first := invitePair(t, f)
	second, err := f.H.store.CreatePairInvitation(
		ctx,
		f.Other,
		"other-invite",
		db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: 1},
	)
	require.NoError(t, err)
	type outcome struct {
		result db.PairResult
		err    error
	}
	results := make(chan outcome, 2)
	var ready sync.WaitGroup
	for _, invitation := range []db.PairResult{first, second} {
		ready.Go(func() {
			result, decisionErr := f.H.store.DecidePairInvitation(
				ctx,
				f.B,
				"accept-"+invitation.Invitation.ID.String(),
				"accept",
				db.PairInvitationDecision{
					EventID:         f.EventID,
					InvitationID:    invitation.Invitation.ID,
					ExpectedVersion: 1,
					Role:            registrations.Follower,
				},
			)
			results <- outcome{result: result, err: decisionErr}
		})
	}
	ready.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		if result.err == nil {
			successes++
			require.NotNil(t, result.result.Pair)
		} else {
			conflicts++
			require.ErrorIs(t, result.err, db.ErrConflict)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	firstState, err := f.H.store.PairInvitation(ctx, f.B, f.EventID, first.Invitation.ID)
	require.NoError(t, err)
	secondState, err := f.H.store.PairInvitation(ctx, f.B, f.EventID, second.Invitation.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"accepted", "pending"}, []string{firstState.State, secondState.State})
}

func TestPairScheduledExpirySurvivesRestartAndIAMLoomOutage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	invitation := invitePair(t, f)
	f.H.stop(t)
	_, err := f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_pair_invitations SET created_at=clock_timestamp()-interval '10 seconds',"+
			"expires_at=clock_timestamp()-interval '1 second' WHERE id=$1",
		invitation.Invitation.ID)
	require.NoError(t, err)
	_, err = f.H.store.ExpirePairInvitations(ctx, db.EventScope{BotID: 999, Organization: "other"}, uuid.Nil)
	require.NoError(t, err)
	before, err := f.H.store.PairInvitation(ctx, f.A, f.EventID, invitation.Invitation.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", before.State)
	require.NoError(t, f.H.store.RevokeUser(ctx, f.B.UserID, true))
	f.H.peers.mu.Lock()
	f.H.peers.iamDown, f.H.peers.loomDown = true, true
	f.H.peers.mu.Unlock()
	f.H.start(t)
	require.Eventually(t, func() bool {
		current, readErr := f.H.store.PairInvitation(ctx, f.A, f.EventID, invitation.Invitation.ID)
		return readErr == nil && current.State == "expired" && current.Version == 2
	}, 5*time.Second, 20*time.Millisecond)
	entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	require.Equal(t, f.Entry.Version+2, entry.Version)
	require.Equal(t, f.Entry.RequestedAt, entry.RequestedAt)
	_, err = f.H.store.PrepareAssignment(
		ctx,
		f.A.EventScope,
		"registration-admin",
		"expired-after-restart",
		db.PassAssignment{
			EventID: f.EventID, RegistrationID: entry.ID, ExpectedEventVersion: 1, ExpectedVersion: entry.Version,
		},
	)
	require.NoError(t, err)
	f.H.stop(t)
	f.H.start(t)
	_, err = f.H.store.ExpirePairInvitations(ctx, f.A.EventScope, uuid.Nil)
	require.NoError(t, err)
	after, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	require.Equal(t, entry, after)
	replay, err := f.H.store.CreatePairInvitation(ctx, f.A, "pair-invite", db.PairInvitationRequest{
		EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version,
	})
	require.NoError(t, err)
	require.Equal(t, invitation, replay)
}

func TestPairExpiredInvitationReleasesHoldAndPreservesReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	invitation := invitePair(t, f)
	f.H.stop(t)
	_, err := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_pair_invitations SET created_at=clock_timestamp()-interval '10 seconds',"+
			"expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", invitation.Invitation.ID)
	require.NoError(t, err)
	args := db.PairInvitationDecision{
		EventID:         f.EventID,
		InvitationID:    invitation.Invitation.ID,
		ExpectedVersion: 1,
		Role:            registrations.Follower,
	}
	expired, err := f.H.store.DecidePairInvitation(ctx, f.B, "expired-accept", "accept", args)
	require.NoError(t, err)
	require.Equal(t, "expired", expired.Invitation.State)
	require.Nil(t, expired.Pair)
	_, err = f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
	require.Error(t, err)
	entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	require.Equal(t, f.Entry.RequestedAt, entry.RequestedAt)
	_, err = f.H.store.PrepareAssignment(
		ctx,
		f.A.EventScope,
		"registration-admin",
		"expired-solo",
		db.PassAssignment{
			EventID:              f.EventID,
			RegistrationID:       entry.ID,
			ExpectedEventVersion: 1,
			ExpectedVersion:      entry.Version,
		},
	)
	require.NoError(t, err)
	replay, err := f.H.store.DecidePairInvitation(ctx, f.B, "expired-accept", "accept", args)
	require.NoError(t, err)
	require.Equal(t, expired, replay)
	_, err = f.H.store.PairInvitation(ctx, f.Other, f.EventID, invitation.Invitation.ID)
	require.ErrorIs(t, err, db.ErrPairDenied)
}

func setupPair(t *testing.T) pairFixture {
	t.Helper()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 12001, true)
	a, token := registrationUser(t, h, 12002, false)
	b, _ := registrationUser(t, h, 12003, false)
	other, _ := registrationUser(t, h, 12004, false)
	event := registrationEvent(t, h, admin)
	entry := registerForEvent(t, h, token, event.ID, "pair-join", registrations.Leader)
	return pairFixture{
		H:       h,
		A:       paymentScope(t, h, a, false),
		B:       paymentScope(t, h, b, false),
		Other:   paymentScope(t, h, other, false),
		EventID: event.ID,
		Entry:   entry,
	}
}

func invitePair(t *testing.T, f pairFixture) db.PairResult {
	t.Helper()
	result, err := f.H.store.CreatePairInvitation(
		context.Background(),
		f.A,
		"pair-invite",
		db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version},
	)
	require.NoError(t, err)
	require.Equal(t, "pending", result.Invitation.State)
	return result
}

func TestPairConsentPreservesSoloHistoryAndBlocksSingletonAssignment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	soloArgs := db.PassAssignment{
		EventID:              f.EventID,
		RegistrationID:       f.Entry.ID,
		ExpectedEventVersion: 1,
		ExpectedVersion:      f.Entry.Version,
	}
	prep, err := f.H.store.PrepareAssignment(ctx, f.A.EventScope, "registration-admin", "old-solo", soloArgs)
	require.NoError(t, err)
	invitation := invitePair(t, f)
	_, err = f.H.store.ConfirmAssignment(ctx, f.A.EventScope, "registration-admin", "old-solo", soloArgs, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	before, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	require.Nil(t, before.Pass)
	require.Equal(t, f.Entry.RequestedAt, before.RequestedAt)
	require.Equal(t, f.Entry.Version+1, before.Version)
	soloArgs.ExpectedVersion = before.Version
	_, err = f.H.store.PrepareAssignment(ctx, f.A.EventScope, "registration-admin", "new-solo", soloArgs)
	require.ErrorIs(t, err, db.ErrConflict)
	args := db.PairInvitationDecision{
		EventID:         f.EventID,
		InvitationID:    invitation.Invitation.ID,
		ExpectedVersion: 1,
		Role:            registrations.Follower,
	}
	_, err = f.H.store.DecidePairInvitation(ctx, f.Other, "not-target", "accept", args)
	require.ErrorIs(t, err, db.ErrPairDenied)
	accepted, err := f.H.store.DecidePairInvitation(ctx, f.B, "pair-accept", "accept", args)
	require.NoError(t, err)
	require.NotNil(t, accepted.Pair)
	require.Equal(t, "accepted", accepted.Invitation.State)
	require.Equal(t, f.Entry.ID, accepted.Pair.LeaderRegistrationID)
	a, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	b, err := f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
	require.NoError(t, err)
	require.Nil(t, a.Pass)
	require.Nil(t, b.Pass)
	require.Equal(t, f.Entry.RequestedAt, a.RequestedAt)
	require.Equal(t, b.RequestedAt, accepted.Pair.QueuedAt)
	require.Equal(t, b.ID, accepted.Pair.FollowerRegistrationID)
	soloArgs.ExpectedVersion = a.Version
	_, err = f.H.store.PrepareAssignment(ctx, f.A.EventScope, "registration-admin", "paired-solo", soloArgs)
	require.ErrorIs(t, err, db.ErrConflict)
	f.H.stop(t)
	f.H.start(t)
	replay, err := f.H.store.DecidePairInvitation(ctx, f.B, "pair-accept", "accept", args)
	require.NoError(t, err)
	require.Equal(t, accepted, replay)
	original, err := f.H.store.CreatePairInvitation(
		ctx,
		f.A,
		"pair-invite",
		db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version},
	)
	require.NoError(t, err)
	require.Equal(t, invitation, original)
	require.NoError(t, f.H.store.RevokeUser(ctx, f.B.UserID, true))
	_, err = f.H.store.DecidePairInvitation(ctx, f.B, "pair-accept", "accept", args)
	require.Error(t, err)
}

func TestPairDeclineAndRevokeReleaseInviterWithoutRegisteringTarget(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{"decline", "revoke"} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := setupPair(t)
			invitation := invitePair(t, f)
			actor := f.B
			if decision == "revoke" {
				actor = f.A
			}
			args := db.PairInvitationDecision{
				EventID:         f.EventID,
				InvitationID:    invitation.Invitation.ID,
				ExpectedVersion: 1,
			}
			result, err := f.H.store.DecidePairInvitation(ctx, actor, "pair-close", decision, args)
			require.NoError(t, err)
			require.Nil(t, result.Pair)
			entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
			require.NoError(t, err)
			require.Equal(t, f.Entry.RequestedAt, entry.RequestedAt)
			require.Equal(t, f.Entry.Version+2, entry.Version)
			_, err = f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
			require.Error(t, err)
			_, err = f.H.store.PrepareAssignment(
				ctx,
				f.A.EventScope,
				"registration-admin",
				"released-solo",
				db.PassAssignment{
					EventID:              f.EventID,
					RegistrationID:       entry.ID,
					ExpectedEventVersion: 1,
					ExpectedVersion:      entry.Version,
				},
			)
			require.NoError(t, err)
			replay, err := f.H.store.DecidePairInvitation(ctx, actor, "pair-close", decision, args)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			otherKind := "revoke"
			if decision == "revoke" {
				otherKind = "decline"
			}
			_, err = f.H.store.DecidePairInvitation(ctx, actor, "pair-close", otherKind, args)
			require.ErrorIs(t, err, db.ErrConflict)
		})
	}
}
