package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func pairIdentityName(t *testing.T, f pairFixture, member db.PaymentScope, name string) {
	t.Helper()
	scope := db.ProfileScope{
		BotID:        member.BotID,
		Organization: member.Organization,
		UserID:       member.UserID,
		Subject:      member.Subject,
	}
	profile, err := f.H.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	_, err = f.H.store.MutateProfile(
		t.Context(),
		scope,
		db.ProfileMutation{OperationID: uuid.New(), ExpectedVersion: profile.Version, Kind: "set_name", FullName: name},
	)
	require.NoError(t, err)
}

func pairIdentitySecondPair(t *testing.T, f pairFixture) {
	t.Helper()
	leaderID, token := registrationUser(t, f.H, 12951, false)
	followerID, _ := registrationUser(t, f.H, 12952, false)
	leader, follower := paymentScope(t, f.H, leaderID, false), paymentScope(t, f.H, followerID, false)
	pairIdentityName(t, f, leader, "Other Leader")
	pairIdentityName(t, f, follower, "Other Follower")
	entry := registerForEvent(t, f.H, token, f.EventID, "other-pair-join", registrations.Leader)
	invite, err := f.H.store.CreatePairInvitation(
		t.Context(),
		leader,
		"other-pair-invite",
		db.PairInvitationRequest{EventID: f.EventID, TargetUserID: followerID, ExpectedVersion: entry.Version},
	)
	require.NoError(t, err)
	_, err = f.H.store.DecidePairInvitation(
		t.Context(),
		follower,
		"other-pair-accept",
		"accept",
		db.PairInvitationDecision{
			EventID:         f.EventID,
			InvitationID:    invite.Invitation.ID,
			ExpectedVersion: 1,
			Role:            registrations.Follower,
		},
	)
	require.NoError(t, err)
}

func TestPairAssignmentIdentityAPIEventAdminRecognizesParticipantsWithoutGlobalLookup(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	pairIdentitySecondPair(t, f.pairFixture)
	_, err := f.H.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"identity-event-admin",
		db.AuthorityChange{EventID: f.EventID, UserID: f.admin.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	scoped := f.admin
	scoped.GlobalAdmin = false
	token := pairUserToken(f.pairFixture, scoped)
	code, raw := pairAdoptionRead(t, f.pairFixture, token, "candidates", map[string]any{"event_id": f.EventID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var listed struct {
		Result struct {
			Candidates []db.PairAssignmentCandidate `json:"candidates"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &listed))
	require.Len(t, listed.Result.Candidates, 2)
	var selected *db.PairAssignmentCandidate
	for i := range listed.Result.Candidates {
		candidate := &listed.Result.Candidates[i]
		if candidate.Leader.DisplayName == "Alex Leader" && candidate.Follower.TelegramUserID == 12003 {
			selected = candidate
		}
	}
	require.NotNil(t, selected, "Select using authorized human identity metadata rather than fixture pair UUID")
	require.Equal(t, f.args, selected.Arguments)
	require.Equal(t, f.A.UserID, selected.Leader.UserID)
	require.Equal(t, selected.LeaderUserID, selected.Leader.UserID)
	require.Equal(t, selected.FollowerUserID, selected.Follower.UserID)
	require.Equal(t, "Sam Follower", selected.Follower.DisplayName)
	require.Equal(t, int64(12002), selected.Leader.TelegramUserID)
	for _, field := range []string{"passport", "legal_name", "price_minor", "zitadel_subject"} {
		require.NotContains(t, string(raw), field)
	}
	code, raw = adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		"/internal/agent/administration/massage_specialist_target",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "no-global-lookup",
			"arguments":        map[string]any{"user_id": f.A.UserID},
		},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	// A native preparation is later readable by the same event administrator's agent.
	prep := pairAssignmentAppPrepare(t, f, "identity-native-original")
	code, raw = pairAdoptionRead(t, f.pairFixture, token, "assignment_proposal", map[string]any{"proposal_id": prep.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var exact struct {
		Result db.PairAssignmentProposalView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &exact))
	require.Equal(t, prep, exact.Result.Preparation)
	require.Equal(t, "Alex Leader", exact.Result.Leader.DisplayName)
	require.Equal(t, int64(12003), exact.Result.Follower.TelegramUserID)
	code, raw = pairAdoptionRead(t, f.pairFixture, token, "assignment_proposals", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var pending struct {
		Result db.PairAssignmentProposalPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &pending))
	require.Len(t, pending.Result.Proposals, 1)
	require.Equal(t, exact.Result.Leader, pending.Result.Proposals[0].Leader)
	require.Equal(t, exact.Result.Follower, pending.Result.Proposals[0].Follower)
	otherArgs := pairAssignmentInAnotherEvent(t, f.pairFixture, f.admin)
	code, raw = pairAdoptionRead(t, f.pairFixture, token, "candidates", map[string]any{"event_id": otherArgs.EventID})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), "Alex Leader")
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestPairAssignmentIdentityAPIRevokedParticipantIsNotCandidateButRetainsHistoricalLocator(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	pairIdentitySecondPair(t, f.pairFixture)
	code, raw := pairAdoptionRead(t, f.pairFixture, f.token, "candidates", map[string]any{"event_id": f.EventID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var listed struct {
		Result struct {
			Candidates []db.PairAssignmentCandidate `json:"candidates"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &listed))
	require.Len(t, listed.Result.Candidates, 2)
	// Revoke the first UUID page so a limit-one read must reach the next active pair.
	first, remaining := listed.Result.Candidates[0], listed.Result.Candidates[1]
	f.args = first.Arguments
	prep := pairAssignmentAppPrepare(t, f, "identity-before-revocation")
	require.NoError(t, f.H.store.RevokeUser(t.Context(), first.Follower.UserID, true))
	code, raw = pairAdoptionRead(
		t,
		f.pairFixture,
		f.token,
		"candidates",
		map[string]any{"event_id": f.EventID, "limit": 1},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &listed))
	require.Len(t, listed.Result.Candidates, 1)
	require.Equal(t, remaining.Arguments, listed.Result.Candidates[0].Arguments)
	require.Equal(t, remaining.Leader, listed.Result.Candidates[0].Leader)
	code, raw = pairAdoptionRead(
		t,
		f.pairFixture,
		f.token,
		"assignment_proposal",
		map[string]any{"proposal_id": prep.ID},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var exact struct {
		Result db.PairAssignmentProposalView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &exact))
	require.Equal(t, prep, exact.Result.Preparation)
	require.Equal(t, first.Follower.UserID, exact.Result.Follower.UserID)
	require.Equal(t, first.Follower.TelegramUserID, exact.Result.Follower.TelegramUserID)
	require.Empty(t, exact.Result.Follower.DisplayName)
	require.NotContains(t, string(raw), first.Follower.DisplayName)
	require.Nil(t, exact.Result.Result)
}
