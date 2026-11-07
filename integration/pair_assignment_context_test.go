package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func pairAssignmentHistoryTask(t *testing.T, h *harness, update, telegramUser int64) db.Task {
	t.Helper()
	entry, _, err := h.store.Intake(t.Context(), db.IntakeRequest{
		BotID: 123, UpdateID: update, TelegramUserID: telegramUser, ChatID: telegramUser,
		Payload: json.RawMessage(`{"kind":"message","text":"Continue pair assignment"}`),
	}, nil)
	require.NoError(t, err)
	return entry
}

func nativePairHistorySession(t *testing.T, f pairFixture, admin db.PaymentScope) db.MiniAppPairAssignmentSession {
	t.Helper()
	session := nativePaymentSession(t, paymentFixture{H: f.H}, admin, 12005)
	return db.MiniAppPairAssignmentSession{
		UserID: admin.UserID, Bearer: session.Bearer, Generation: session.Generation,
		AuthorityUnchanged: func() bool { return true },
	}
}

func TestNativePairAssignmentContextPreservesOriginalVersionsCutoffAndOwnership(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	ctx := t.Context()
	session := nativePairHistorySession(t, f, admin)
	owner := db.ProfileScope{
		BotID:        admin.BotID,
		Organization: admin.Organization,
		UserID:       admin.UserID,
		Subject:      admin.Subject,
	}
	before := pairAssignmentHistoryTask(t, f.H, 99610, 12005)
	prep, err := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "pair-context", args, uuid.Nil, session)
	require.NoError(t, err)
	after := pairAssignmentHistoryTask(t, f.H, 99611, 12005)
	found, err := f.H.store.HasNativePairAssignmentContext(ctx, owner, before.ID)
	require.NoError(t, err)
	require.False(t, found)
	found, err = f.H.store.HasNativePairAssignmentContext(ctx, owner, after.ID)
	require.NoError(t, err)
	require.True(t, found)
	original, err := f.H.store.RecentNativePairAssignmentActions(ctx, admin, after.ID)
	require.NoError(t, err)
	require.Len(t, original, 1)
	require.Equal(t, prep.ID, original[0].ProposalID)
	require.Equal(t, "pair-context", original[0].CanonicalOperationID)
	require.Equal(t, args.EventID, original[0].EventID)
	require.Equal(t, args.PairID, original[0].PairID)
	require.Equal(t, "prepare", original[0].Kind)
	require.Equal(t, args.ExpectedPairVersion, original[0].PairVersion)
	require.Equal(t, args.ExpectedLeaderVersion, original[0].LeaderVersion)
	require.Equal(t, args.ExpectedFollowerVersion, original[0].FollowerVersion)
	for i := range 6 {
		_, err = f.H.store.PrepareMiniAppPairAssignment(
			ctx,
			admin,
			fmt.Sprintf("pair-context-alias-%d", i),
			args,
			prep.ID,
			session,
		)
		require.NoError(t, err)
	}
	assigned, err := f.H.store.ConfirmMiniAppPairAssignment(ctx, admin, "pair-context", args, prep.ID, session)
	require.NoError(t, err)
	for range 3 {
		replay, e := f.H.store.ConfirmMiniAppPairAssignment(ctx, admin, "pair-context", args, prep.ID, session)
		require.NoError(t, e)
		require.Equal(t, assigned, replay)
	}
	unchanged, err := f.H.store.RecentNativePairAssignmentActions(ctx, admin, after.ID)
	require.NoError(t, err)
	require.Equal(t, original, unchanged, "Later aliases and confirmation cannot enter an earlier task")
	latest := pairAssignmentHistoryTask(t, f.H, 99612, 12005)
	actions, err := f.H.store.RecentNativePairAssignmentActions(ctx, admin, latest.ID)
	require.NoError(t, err)
	require.Len(t, actions, 5)
	require.Equal(t, "confirm", actions[0].Kind)
	require.Equal(t, assigned.Pair.Version, actions[0].PairVersion)
	require.Equal(t, assigned.Leader.Version, actions[0].LeaderVersion)
	require.Equal(t, assigned.Follower.Version, actions[0].FollowerVersion)
	var journalCount int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx,
		"SELECT count(*) FROM shuttle_miniapp_pair_assignment_actions WHERE user_id=$1", admin.UserID).
		Scan(&journalCount))
	require.Equal(t, 8, journalCount, "Receipt replay must preserve the original historical entry")
	foreignTask := pairAssignmentHistoryTask(t, f.H, 99613, 12002)
	hidden, err := f.H.store.RecentNativePairAssignmentActions(ctx, admin, foreignTask.ID)
	require.NoError(t, err)
	require.Empty(t, hidden)
	hidden, err = f.H.store.RecentNativePairAssignmentActions(ctx, f.A, latest.ID)
	require.NoError(t, err)
	require.Empty(t, hidden)
	spoofed := admin
	spoofed.UserID = f.A.UserID
	_, err = f.H.store.RecentNativePairAssignmentActions(ctx, spoofed, latest.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, f.H.store.RevokeUser(ctx, admin.UserID, true))
	_, err = f.H.store.RecentNativePairAssignmentActions(ctx, admin, latest.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	f.H.peers.mu.Lock()
	require.Empty(t, f.H.peers.runs, "Native writes and history reads must not create proactive Loom work")
	f.H.peers.mu.Unlock()
}

func TestNativePairAssignmentContextFiltersCurrentEventAuthorityBeforeLimit(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	ctx := t.Context()
	session := nativePairHistorySession(t, f, admin)
	visible, err := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "visible-pair-history", args, uuid.Nil, session)
	require.NoError(t, err)
	otherArgs := pairAssignmentInAnotherEvent(t, f, admin)
	for i := range 7 {
		_, err = f.H.store.PrepareMiniAppPairAssignment(
			ctx,
			admin,
			fmt.Sprintf("hidden-pair-history-%d", i),
			otherArgs,
			uuid.Nil,
			session,
		)
		require.NoError(t, err)
	}
	authority, err := f.H.store.ChangeAuthority(ctx, admin.EventScope, admin.Subject, "grant-pair-history-event",
		db.AuthorityChange{EventID: args.EventID, UserID: admin.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	entry := pairAssignmentHistoryTask(t, f.H, 99620, 12005)
	scoped := admin
	scoped.GlobalAdmin = false
	actions, err := f.H.store.RecentNativePairAssignmentActions(ctx, scoped, entry.ID)
	require.NoError(t, err)
	require.Len(t, actions, 1, "Newer unauthorized events must not hide earlier authorized history")
	require.Equal(t, visible.ID, actions[0].ProposalID)
	_, err = f.H.store.ChangeAuthority(
		ctx,
		admin.EventScope,
		admin.Subject,
		"downgrade-pair-history-event",
		db.AuthorityChange{
			EventID:         args.EventID,
			UserID:          admin.UserID,
			Role:            db.AuthorityAmbassador,
			ExpectedVersion: authority.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	actions, err = f.H.store.RecentNativePairAssignmentActions(ctx, scoped, entry.ID)
	require.NoError(t, err)
	require.Empty(t, actions)
	actions, err = f.H.store.RecentNativePairAssignmentActions(ctx, admin, entry.ID)
	require.NoError(t, err)
	require.Len(t, actions, 5)
	wrongScope := admin
	wrongScope.Organization = "another-org"
	_, err = f.H.store.RecentNativePairAssignmentActions(ctx, wrongScope, entry.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestNativePairAssignmentContextReachesExplicitAgentTurnForEventAdministrator(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	ctx := t.Context()
	session := nativePairHistorySession(t, f, admin)
	prep, err := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "pair-agent-history", args, uuid.Nil, session)
	require.NoError(t, err)
	_, err = f.H.store.ChangeAuthority(ctx, admin.EventScope, admin.Subject, "pair-agent-event-admin",
		db.AuthorityChange{EventID: args.EventID, UserID: admin.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	f.H.peers.mu.Lock()
	require.Empty(t, f.H.peers.runs)
	f.H.peers.adminRole = false
	f.H.peers.mu.Unlock()
	f.H.stop(t)
	f.H.start(t)
	accepted := paymentReviewMessage(t, f.H, 99630, 12005, "Continue the pair assignment I prepared in the form")
	f.H.await(t, accepted, "completed")
	f.H.peers.mu.Lock()
	prompt := f.H.peers.createdInputs[len(f.H.peers.createdInputs)-1]
	runs := len(f.H.peers.runs)
	f.H.peers.mu.Unlock()
	require.Equal(t, 1, runs)
	require.Contains(t, prompt, "Recent native pair pass assignment actions")
	require.Contains(t, prompt, prep.ID.String())
	require.Contains(t, prompt, "zinusya_pairs.assignment_proposal")
	require.Contains(t, prompt, "resume_proposal_id")
	require.Contains(t, prompt, "fresh exact human approval")
	_, err = f.H.store.Pool().Exec(ctx,
		`DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2`, args.EventID, admin.UserID)
	require.NoError(t, err)
	accepted = paymentReviewMessage(t, f.H, 99631, 12005, "Check which pair assignments I can manage now")
	f.H.await(t, accepted, "completed")
	f.H.peers.mu.Lock()
	prompt = f.H.peers.createdInputs[len(f.H.peers.createdInputs)-1]
	f.H.peers.mu.Unlock()
	require.NotContains(t, prompt, "Recent native pair pass assignment actions")
	require.NotContains(t, prompt, prep.ID.String())
}
