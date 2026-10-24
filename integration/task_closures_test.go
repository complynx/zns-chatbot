package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func taskClosureFixture(t *testing.T) (*harness, db.AdminScope, db.TaskClosureArguments) {
	t.Helper()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	id := unknownTask(t, h)
	h.stop(t)
	ctx := context.Background()
	task, err := h.store.GetTask(ctx, id)
	require.NoError(t, err)
	user, err := h.store.GetUser(ctx, task.UserID)
	require.NoError(t, err)
	scope := db.AdminScope{
		BotID: user.BotID, Organization: user.Organization, Subject: user.Subject, UserID: user.ID, GlobalAdmin: true,
	}
	return h, scope, db.TaskClosureArguments{
		TaskID: id, ExpectedVersion: task.Version, Reason: "Checked the lost receipt; stop local processing only",
	}
}

func taskClosureDiagnostics(t *testing.T, h *harness, scope db.AdminScope, id uuid.UUID) db.TaskDiagnostics {
	t.Helper()
	result, err := h.store.TaskDiagnostics(context.Background(), id, scope.BotID, scope.Organization)
	require.NoError(t, err)
	return result
}

func sameTaskClosureJSON(t *testing.T, want, got any) {
	t.Helper()
	expected, err := json.Marshal(want)
	require.NoError(t, err)
	actual, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(actual))
}

func TestTaskClosureConcurrentConfirmationPreservesDeliveryAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	ctx := context.Background()
	before := taskClosureDiagnostics(t, h, scope, args.TaskID)
	_, err := h.store.Pool().Exec(ctx, `INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload,state)
        VALUES ($1,$4,'closure-pending',1,'{}','pending'),
               ($2,$4,'closure-retryable',2,'{}','retryable'),
               ($3,$4,'closure-inflight',3,'{}','send_inflight')`, uuid.New(), uuid.New(), uuid.New(), args.TaskID)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, `UPDATE shuttle_conversations SET active_task_id=$1,active_run_id='original-remote-run'
        WHERE id=(SELECT conversation_id FROM shuttle_tasks WHERE id=$1)`, args.TaskID)
	require.NoError(t, err)
	prep, err := h.store.PrepareTaskClosure(ctx, scope, "closure-concurrent", args)
	require.NoError(t, err)
	require.Contains(t, prep.Description, "does not establish the external outcome")
	repeated, err := h.store.PrepareTaskClosure(ctx, scope, "closure-concurrent", args)
	require.NoError(t, err)
	sameTaskClosureJSON(t, prep, repeated)
	_, err = h.store.TaskClosureReceipt(ctx, scope, "closure-concurrent")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	type outcome struct {
		result db.TaskClosureResult
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, confirmErr := h.store.ConfirmTaskClosure(ctx, scope, "closure-concurrent", args, prep.ID)
			results <- outcome{result, confirmErr}
		}()
	}
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	sameTaskClosureJSON(t, first.result, second.result)
	after := taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.Equal(t, "closed", after.Task.State)
	require.Equal(t, before.Task.Version+1, after.Task.Version)
	require.Equal(t, before.Task.Fence+1, after.Task.Fence)
	require.Len(t, after.Resolutions, 1)
	require.Equal(
		t,
		[]string{"delivery_unknown", "delivery_blocked", "delivery_blocked", "send_inflight"},
		[]string{
			after.Deliveries[0].State,
			after.Deliveries[1].State,
			after.Deliveries[2].State,
			after.Deliveries[3].State,
		},
	)
	var cleared bool
	require.NoError(t, h.store.Pool().QueryRow(ctx,
		"SELECT active_task_id IS NULL AND active_run_id='' FROM shuttle_conversa" +
"tions WHERE id=$1", before.Task.ConversationID).
		Scan(&cleared))
	require.True(t, cleared)
	_, err = h.store.Pool().Exec(ctx,
		"UPDATE shuttle_task_closure_proposals SET expires_at=clock_timestamp()-interval '1 second'")
	require.NoError(t, err)
	reopened, err := db.Open(ctx, h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	receipt, err := reopened.TaskClosureReceipt(ctx, scope, "closure-concurrent")
	require.NoError(t, err)
	sameTaskClosureJSON(t, first.result, receipt)
	replayed, err := reopened.ConfirmTaskClosure(ctx, scope, "closure-concurrent", args, prep.ID)
	require.NoError(t, err)
	sameTaskClosureJSON(t, first.result, replayed)
}

func TestTaskClosureRejectsChangedExpiredLeasedAndResolvedTargets(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"changed_reason", "changed_version", "changed_target", "changed_proposal", "expired", "leased", "stale", "resolved"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			h, scope, args := taskClosureFixture(t)
			ctx := context.Background()
			prep, err := h.store.PrepareTaskClosure(ctx, scope, "closure-rejected", args)
			require.NoError(t, err)
			proposal := prep.ID
			switch scenario {
			case "changed_reason":
				args.Reason = "Another decision"
			case "changed_version":
				args.ExpectedVersion++
			case "changed_target":
				args.TaskID = uuid.New()
			case "changed_proposal":
				proposal = uuid.New()
			case "expired":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_task_closure_proposals SET expires_at=clock_timestamp()-interval '1 second'")
			case "leased":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_tasks SET lease_owner='live-worker',lease_until=clock_tim" +
"estamp()+interval '1 hour' WHERE id=$1", args.TaskID)
			case "stale":
				_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_tasks SET version=version+1 WHERE id=$1", args.TaskID)
			case "resolved":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_outbox SET state='delivered' WHERE task_id=$1", args.TaskID)
			}
			require.NoError(t, err)
			_, err = h.store.ConfirmTaskClosure(ctx, scope, "closure-rejected", args, proposal)
			require.ErrorIs(t, err, db.ErrConflict)
			_, err = h.store.TaskClosureReceipt(ctx, scope, "closure-rejected")
			require.ErrorIs(t, err, pgx.ErrNoRows)
			after := taskClosureDiagnostics(t, h, scope, prep.Arguments.TaskID)
			require.NotEqual(t, "closed", after.Task.State)
			require.Empty(t, after.Resolutions)
		})
	}
}

func TestTaskClosureDoesNotAdoptOperatorReceipt(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	ctx := context.Background()
	prep, err := h.store.PrepareTaskClosure(ctx, scope, "shared-operation", args)
	require.NoError(t, err)
	_, err = h.store.CloseUncertainTask(ctx, db.CloseTaskRequest{
		TaskID: args.TaskID, BotID: scope.BotID, Organization: scope.Organization, Actor: scope.Subject,
		OperationID: "shared-operation", Reason: args.Reason, ExpectedVersion: args.ExpectedVersion,
	})
	require.NoError(t, err)
	_, err = h.store.ConfirmTaskClosure(ctx, scope, "shared-operation", args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.TaskClosureReceipt(ctx, scope, "shared-operation")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	after := taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.Len(t, after.Resolutions, 1)
	require.Equal(t, "shared-operation", after.Resolutions[0].OperationID)
}

func TestTaskClosureRequiresActiveExactActorAndScope(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	ctx := context.Background()
	prep, err := h.store.PrepareTaskClosure(ctx, scope, "scope-bound", args)
	require.NoError(t, err)
	for _, change := range []func(*db.AdminScope){
		func(s *db.AdminScope) { s.GlobalAdmin = false },
		func(s *db.AdminScope) { s.UserID = uuid.New() },
		func(s *db.AdminScope) { s.Subject = "other-subject" },
		func(s *db.AdminScope) { s.Organization = "other-org" },
		func(s *db.AdminScope) { s.BotID++ },
	} {
		foreign := scope
		change(&foreign)
		_, err = h.store.ConfirmTaskClosure(ctx, foreign, "scope-bound", args, prep.ID)
		require.ErrorIs(t, err, db.ErrAdminDenied)
		_, err = h.store.TaskClosureReceipt(ctx, foreign, "scope-bound")
		require.ErrorIs(t, err, db.ErrAdminDenied)
	}
	_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = h.store.ConfirmTaskClosure(ctx, scope, "scope-bound", args, prep.ID)
	require.ErrorIs(t, err, db.ErrAdminDenied)
	_, err = h.store.TaskClosureReceipt(ctx, scope, "scope-bound")
	require.ErrorIs(t, err, db.ErrAdminDenied)
	after := taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.NotEqual(t, "closed", after.Task.State)
	require.Empty(t, after.Resolutions)
}

func TestTaskClosureResultFailureRollsBackEffectsAndCanRetrySameProposal(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	ctx := context.Background()
	before := taskClosureDiagnostics(t, h, scope, args.TaskID)
	prep, err := h.store.PrepareTaskClosure(ctx, scope, "closure-rollback", args)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, `CREATE FUNCTION reject_task_closure_result() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'synthetic receipt persistence failure'; END $$;
        CREATE TRIGGER reject_task_closure_result BEFORE UPDATE ON shuttle_task_closure_proposals
        FOR EACH ROW EXECUTE FUNCTION reject_task_closure_result()`)
	require.NoError(t, err)
	_, err = h.store.ConfirmTaskClosure(ctx, scope, "closure-rollback", args, prep.ID)
	require.Error(t, err)
	after := taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.Equal(t, before, after)
	_, err = h.store.TaskClosureReceipt(ctx, scope, "closure-rollback")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.Pool().Exec(ctx, `DROP TRIGGER reject_task_closure_result ON shuttle_task_closure_proposals;
        DROP FUNCTION reject_task_closure_result()`)
	require.NoError(t, err)
	_, err = h.store.ConfirmTaskClosure(ctx, scope, "closure-rollback", args, prep.ID)
	require.NoError(t, err)
	after = taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.Equal(t, "closed", after.Task.State)
	require.Len(t, after.Resolutions, 1)
}

func closureIntake(t *testing.T, h *harness, bot, telegramID, updateID int64) db.Task {
	t.Helper()
	task, duplicate, err := h.store.Intake(context.Background(), db.IntakeRequest{
		BotID: bot, TelegramUserID: telegramID, ChatID: telegramID, UpdateID: updateID, Payload: json.RawMessage(`{}`),
	}, nil)
	require.NoError(t, err)
	require.False(t, duplicate)
	return task
}

func TestTaskClosureHidesForeignTargetsAndSeparatesValidActors(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	ctx := context.Background()
	otherTask := closureIntake(t, h, scope.BotID, 987, 987)
	_, err := h.store.BindSubject(ctx, otherTask.UserID, "another-admin", scope.Organization)
	require.NoError(t, err)
	other := scope
	other.Subject, other.UserID = "another-admin", otherTask.UserID
	prep, err := h.store.PrepareTaskClosure(ctx, scope, "same-operation", args)
	require.NoError(t, err)
	_, err = h.store.TaskClosureReceipt(ctx, other, "same-operation")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.ConfirmTaskClosure(ctx, other, "same-operation", args, prep.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	otherPrep, err := h.store.PrepareTaskClosure(ctx, other, "same-operation", args)
	require.NoError(t, err)
	require.NotEqual(t, prep.ID, otherPrep.ID)
	_, err = h.store.ConfirmTaskClosure(ctx, scope, "same-operation", args, prep.ID)
	require.NoError(t, err)
	_, err = h.store.ConfirmTaskClosure(ctx, other, "same-operation", args, otherPrep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.TaskClosureReceipt(ctx, other, "same-operation")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	foreignBot := closureIntake(t, h, scope.BotID+1, 988, 988)
	foreignOrg := closureIntake(t, h, scope.BotID, 989, 989)
	_, err = h.store.BindSubject(ctx, foreignOrg.UserID, "foreign-target", "foreign-org")
	require.NoError(t, err)
	for _, target := range []db.Task{foreignBot, foreignOrg} {
		foreignArgs := args
		foreignArgs.TaskID, foreignArgs.ExpectedVersion = target.ID, target.Version
		_, err = h.store.PrepareTaskClosure(ctx, scope, uuid.NewString(), foreignArgs)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
}

func TestTaskClosureKeepsNewerConversationBinding(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	ctx := context.Background()
	user, err := h.store.GetUser(ctx, scope.UserID)
	require.NoError(t, err)
	newer := closureIntake(t, h, scope.BotID, user.TelegramUserID, 990)
	_, err = h.store.Pool().Exec(ctx,
		"UPDATE shuttle_conversations SET active_task_id=$1,active_run_id='newer-" +
"remote-run' WHERE id=$2", newer.ID, newer.ConversationID)
	require.NoError(t, err)
	prep, err := h.store.PrepareTaskClosure(ctx, scope, "close-older", args)
	require.NoError(t, err)
	_, err = h.store.ConfirmTaskClosure(ctx, scope, "close-older", args, prep.ID)
	require.NoError(t, err)
	var task uuid.UUID
	var run string
	require.NoError(t, h.store.Pool().QueryRow(ctx,
		"SELECT active_task_id,active_run_id FROM shuttle_conversations WHERE id=$1", newer.ConversationID).
		Scan(&task, &run))
	require.Equal(t, newer.ID, task)
	require.Equal(t, "newer-remote-run", run)
}
