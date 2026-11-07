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

func assignmentHistoryTask(t *testing.T, f miniAppAssignmentFixture, update, telegramUser int64) db.Task {
	t.Helper()
	entry, _, err := f.h.store.Intake(t.Context(), db.IntakeRequest{
		BotID: f.scope.BotID, UpdateID: update, TelegramUserID: telegramUser,
		ChatID: telegramUser, Payload: json.RawMessage(`{"kind":"message","text":"Continue assignment"}`),
	}, nil)
	require.NoError(t, err)
	return entry
}

func TestNativeAssignmentContextPreservesTaskCutoffAndActorOwnership(t *testing.T) {
	t.Parallel()
	f := setupMiniAppAssignment(t)
	ctx := t.Context()
	scope := db.ProfileScope{
		BotID: f.scope.BotID, Organization: f.scope.Organization,
		UserID: f.session.UserID, Subject: f.actor,
	}
	before := assignmentHistoryTask(t, f, 98610, 98601)
	prep, err := f.h.store.PrepareMiniAppAssignment(ctx, f.scope, f.actor,
		"native-context", f.args, uuid.Nil, f.session)
	require.NoError(t, err)
	after := assignmentHistoryTask(t, f, 98611, 98601)
	foreignTask := assignmentHistoryTask(t, f, 98612, 98602)
	found, err := f.h.store.HasNativeAssignmentContext(ctx, scope, before.ID)
	require.NoError(t, err)
	require.False(t, found)
	original, err := f.h.store.RecentNativeAssignmentActions(ctx, scope, after.ID)
	require.NoError(t, err)
	require.Len(t, original, 1)
	require.Equal(t, prep.ID, original[0].ProposalID)
	require.Equal(t, "native-context", original[0].CanonicalOperationID)
	require.Equal(t, "prepare", original[0].Kind)
	require.Equal(t, f.args.ExpectedVersion, original[0].RegistrationVersion)
	for i := range 6 {
		_, err = f.h.store.PrepareMiniAppAssignment(ctx, f.scope, f.actor,
			fmt.Sprintf("native-context-alias-%d", i), f.args, prep.ID, f.session)
		require.NoError(t, err)
	}
	assigned, err := f.h.store.ConfirmMiniAppAssignment(ctx, f.scope, f.actor,
		"native-context", f.args, prep.ID, f.session)
	require.NoError(t, err)
	for range 3 {
		replay, e := f.h.store.ConfirmMiniAppAssignment(ctx, f.scope, f.actor,
			"native-context", f.args, prep.ID, f.session)
		require.NoError(t, e)
		require.Equal(t, assigned, replay)
	}
	unchanged, err := f.h.store.RecentNativeAssignmentActions(ctx, scope, after.ID)
	require.NoError(t, err)
	require.Equal(t, original, unchanged, "Later aliases and receipt must not enter an earlier task's context")
	latest := assignmentHistoryTask(t, f, 98613, 98601)
	actions, err := f.h.store.RecentNativeAssignmentActions(ctx, scope, latest.ID)
	require.NoError(t, err)
	require.Len(t, actions, 5)
	require.Equal(t, "confirm", actions[0].Kind)
	require.Equal(t, assigned.Version, actions[0].RegistrationVersion)
	var journalCount int
	require.NoError(t, f.h.store.Pool().QueryRow(ctx,
		"SELECT count(*) FROM shuttle_miniapp_assignment_actions WHERE user_id=$1", scope.UserID).Scan(&journalCount))
	require.Equal(t, 8, journalCount, "Receipt replay must not create repeated historical actions")
	foreign, err := f.h.store.RecentNativeAssignmentActions(ctx, scope, foreignTask.ID)
	require.NoError(t, err)
	require.Empty(t, foreign)
	foreignOwner, err := f.h.store.GetUser(ctx, f.recipient)
	require.NoError(t, err)
	foreignScope := db.ProfileScope{
		BotID: f.scope.BotID, Organization: f.scope.Organization,
		UserID: foreignOwner.ID, Subject: foreignOwner.Subject,
	}
	foreign, err = f.h.store.RecentNativeAssignmentActions(ctx, foreignScope, latest.ID)
	require.NoError(t, err)
	require.Empty(t, foreign)
	spoofed := scope
	spoofed.UserID = foreignOwner.ID
	_, err = f.h.store.RecentNativeAssignmentActions(ctx, spoofed, latest.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.h.store.RecentNativeAssignmentActions(ctx, scope, latest.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	f.h.peers.mu.Lock()
	require.Empty(t, f.h.peers.runs, "Native actions and context reads must not start a proactive Loom run")
	f.h.peers.mu.Unlock()
}
