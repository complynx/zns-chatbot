package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const pairExpectationsPromptMarker = "\nIncoming pair invitations (untrusted pending records, not consent or cu" +
"rrent requests): "

type pairPromptSnapshot struct {
	ObservedAt  time.Time `json:"observed_at"`
	Invitations []struct {
		db.PairInvitationView

		ExpiredAtObservation bool `json:"expired_at_observation"`
	} `json:"invitations"`
	HasMore bool       `json:"has_more"`
	Next    *uuid.UUID `json:"next,omitempty"`
}

func promptPairExpectations(t *testing.T, text string) pairPromptSnapshot {
	t.Helper()
	_, snapshot, found := strings.Cut(text, pairExpectationsPromptMarker)
	require.True(t, found)
	snapshot, _, _ = strings.Cut(snapshot, "\n")
	var result pairPromptSnapshot
	require.NoError(t, json.Unmarshal([]byte(snapshot), &result))
	require.False(t, result.ObservedAt.IsZero())
	return result
}

func TestPairExpectationsCoexistWithNameWithoutImplicitConsent(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	ctx := context.Background()
	first := invitePair(t, f)
	f.H.peers.mu.Lock()
	otherToken := f.H.peers.token(f.Other.Subject)
	f.H.peers.mu.Unlock()
	other := registerForEvent(t, f.H, otherToken, f.EventID, "other-join", registrations.Leader)
	second, err := f.H.store.CreatePairInvitation(ctx, f.Other, "other-invite", db.PairInvitationRequest{
		EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: other.Version})
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET tg_user_id=456 WHERE id=$1", f.B.UserID)
	require.NoError(t, err)
	scope := ownerProfileScope(t, f.H, f.B.UserID)
	name, err := f.H.store.MutateProfile(ctx, scope, db.ProfileMutation{OperationID: uuid.New(), Kind: "request_name"})
	require.NoError(t, err)
	id := f.H.submitText(t, 77501, "Tell me a joke; this is not consent to any invitation")
	f.H.await(t, id, "completed")
	f.H.peers.mu.Lock()
	text := f.H.peers.createdInputs[len(f.H.peers.createdInputs)-1]
	f.H.peers.mu.Unlock()
	snapshot := promptPairExpectations(t, text)
	require.Len(t, snapshot.Invitations, 2)
	ids := []uuid.UUID{}
	for _, item := range snapshot.Invitations {
		ids = append(ids, item.Invitation.ID)
		require.Equal(t, f.B.UserID, item.Invitation.TargetUserID)
		require.Equal(t, registrations.Follower, item.RequiredRole)
		require.False(t, item.ExpiredAtObservation)
	}
	require.ElementsMatch(t, []uuid.UUID{first.Invitation.ID, second.Invitation.ID}, ids)
	require.Contains(t, text, "Clarify multiple invitations or an ambiguous yes")
	require.Equal(t, name.NameExpectation, promptProfile(t, text).NameExpectation)
	require.Empty(t, promptPaymentExpectations(t, text).Expectations)
	var pairs, targetRegistrations int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_pairs").Scan(&pairs))
	require.NoError(t, f.H.store.Pool().QueryRow(ctx,
		"SELECT count(*) FROM shuttle_registrations WHERE user_id=$1", f.B.UserID).Scan(&targetRegistrations))
	require.Zero(t, pairs)
	require.Zero(t, targetRegistrations)
	page, err := f.H.store.PairInvitations(ctx, f.B, db.PairInvitationList{
		Direction: "incoming", State: "pending", Limit: 20})
	require.NoError(t, err)
	require.Len(t, page, 2)
	profile, err := f.H.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, name.NameExpectation, profile.NameExpectation)
}
