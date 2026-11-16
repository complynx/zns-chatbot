package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// RecoveryOutputUnchanged avoids version churn for a parked worker. Check the
// same persisted fields as SaveTask, under its lease, plus all intended outbox keys.
func (s *Store) RecoveryOutputUnchanged(ctx context.Context, t Task, owner string,
	snapshot []byte, keys []string) (bool, error) {
	if terminalTask(t.State) {
		return false, nil // Save must still release any terminal conversation pointer.
	}
	var unchanged bool
	err := s.pool.QueryRow(ctx, `SELECT state=$4 AND run_id=$5 AND auth_session_id=$6 AND question_id=$7
	 AND snapshot=$8::jsonb AND NOT EXISTS(SELECT 1 FROM unnest($9::text[]) k
	 WHERE NOT EXISTS(SELECT 1 FROM shuttle_outbox o WHERE o.task_id=$1 AND o.dedup_key=k))
	 AND NOT EXISTS(SELECT 1 FROM shuttle_conversations c WHERE c.id=t.conversation_id AND c.active_task_id=t.id
	 AND (c.active_run_id<>$5 OR c.awaiting_question_id<>$7))
	 FROM shuttle_tasks t WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>now()`,
		t.ID, owner, t.Fence, t.State, t.RunID, t.AuthSessionID, t.QuestionID, snapshot, keys).Scan(&unchanged)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrLeaseLost
	}
	return unchanged, err
}

// RecoveryReady preserves source order before accepting text at Loom's chat inbox.
// A run's observer does not block later steering; an undelivered input does.
func (s *Store) RecoveryReady(ctx context.Context, t Task) (bool, int64, error) {
	var ready bool
	var seq int64
	err := s.pool.QueryRow(ctx, `SELECT NOT EXISTS (
	 SELECT 1 FROM shuttle_tasks WHERE conversation_id=$1 AND seq<$2
	 AND route IN ('new_run','input','approval','cancel')
	 AND state NOT IN ('completed','failed','cancelled','closed','delivery_unknown','delivery_failed')
	 AND (route<>'new_run' OR run_id='')
	), COALESCE((SELECT max(seq) FROM shuttle_tasks WHERE conversation_id=$1
	 AND coalesce(request->>'start_continuation','')=''),0)`,
		t.ConversationID, t.Seq).Scan(&ready, &seq)
	return ready, seq, err
}

// RecoveryRunSession retains the original authorization receipt after a lost local pointer.
func (s *Store) RecoveryRunSession(ctx context.Context, user uuid.UUID, run, session string) (json.RawMessage, error) {
	var snapshot json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT snapshot FROM shuttle_tasks WHERE user_id=$1 AND
	 (run_id=$2 OR ($3<>'' AND auth_session_id=$3))
	 AND ($3='' OR snapshot#>>'{session,auth_session_id}'=$3)
	 ORDER BY COALESCE((snapshot->'session'->>'version')::bigint,0) DESC,
	 (run_id=$2) DESC,seq DESC LIMIT 1`, user, run, session).Scan(&snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return snapshot, err
}

// RecoveryObserver resolves current ownership without changing a message's intake target.
func (s *Store) RecoveryObserver(ctx context.Context, t Task) (Task, error) {
	row, err := s.q.GetRecoveryObserver(ctx, sqlc.GetRecoveryObserverParams{
		ConversationID: t.ConversationID, UserID: t.UserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, nil
	}
	return task(row), err
}

// OwnRecoveredRun transfers observation only after Loom has returned a delivered receipt.
// Fence the former observer so a late save cannot resurrect its superseded run.
func (s *Store) OwnRecoveredRun(
	ctx context.Context,
	t Task,
	owner string,
	snapshot json.RawMessage,
) (Task, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return t, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	c, err := q.LockConversation(ctx, t.ConversationID)
	if err != nil {
		return t, false, err
	}
	var observed uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM shuttle_tasks WHERE conversation_id=$1
	 AND run_id=$2 AND id<>$3 AND route='new_run'
	 AND (state IN ('completed','cancelled') OR
	 (state IN ('failed','delivery_unknown','delivery_failed') AND
	 snapshot#>>'{run,status}' IN ('completed','succeeded','failed','cancelled','canceled')))
	 ORDER BY seq DESC LIMIT 1`,
		t.ConversationID, t.RunID, t.ID).Scan(&observed)
	if err == nil {
		return retargetRecoveryInput(ctx, tx, t, owner, observed)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return t, false, err
	}
	active, err := activeTask(ctx, q, c.ActiveTaskID)
	if err != nil {
		return t, false, err
	}
	if recoveryAuthorizationUnknown(active) {
		return t, false, ErrRecoveryAuthorizationUnknown
	}
	if active.ID != uuid.Nil && active.ID != t.ID && active.RunID == t.RunID {
		return retargetRecoveryInput(ctx, tx, t, owner, active.ID)
	}
	snapshot, err = carryRecoveryOnboarding(ctx, tx, t, snapshot)
	if err != nil {
		return t, false, err
	}
	if err = fenceRecoveryPredecessor(ctx, tx, active, t, snapshot); err != nil {
		return t, false, err
	}
	result, err := tx.Exec(ctx, `UPDATE shuttle_tasks SET route='new_run',target_task_id=$4,
	 target_run_id='',run_id=$5,auth_session_id=$6,
	 snapshot=jsonb_set($7::jsonb,'{recovery}',COALESCE($7::jsonb->'recovery','{}'::jsonb)||
	 jsonb_build_object('observer_run_id',$5::text),true),state='awaiting_service',version=version+1
	 WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>now()`,
		t.ID, owner, t.Fence, uuid.Nil, t.RunID, t.AuthSessionID, snapshot)
	if err != nil {
		return t, false, err
	}
	if result.RowsAffected() != 1 {
		return t, false, ErrLeaseLost
	}
	if err = q.ActivatePromotedTask(
		ctx,
		sqlc.ActivatePromotedTaskParams{ID: t.ConversationID, ActiveTaskID: pgtype.UUID{Bytes: t.ID, Valid: true}},
	); err != nil {
		return t, false, err
	}
	if err = q.UpdateConversationRun(
		ctx,
		sqlc.UpdateConversationRunParams{
			ID:           t.ConversationID,
			ActiveTaskID: pgtype.UUID{Bytes: t.ID, Valid: true},
			ActiveRunID:  t.RunID,
		},
	); err != nil {
		return t, false, err
	}
	row, err := q.GetTask(ctx, t.ID)
	if err != nil {
		return t, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return t, false, err
	}
	return task(row), true, nil
}

// A delivered input follows its actual observer for deferred onboarding work.
// Its immutable source request, message body and idempotency key do not change.
func retargetRecoveryInput(
	ctx context.Context,
	tx pgx.Tx,
	t Task,
	owner string,
	observer uuid.UUID,
) (Task, bool, error) {
	if t.TargetTaskID == observer {
		return t, false, nil
	}
	result, err := tx.Exec(ctx, `UPDATE shuttle_tasks SET target_task_id=$4,target_run_id=$5,version=version+1
	 WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>now()`,
		t.ID, owner, t.Fence, observer, t.RunID)
	if err != nil {
		return t, false, err
	}
	if result.RowsAffected() != 1 {
		return t, false, ErrLeaseLost
	}
	row, err := sqlc.New(tx).GetTask(ctx, t.ID)
	if err != nil {
		return t, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return t, false, err
	}
	return task(row), false, nil
}

func fenceRecoveryPredecessor(ctx context.Context, tx pgx.Tx, active sqlc.ShuttleTask,
	next Task, snapshot json.RawMessage) error {
	if active.ID == uuid.Nil || active.ID == next.ID {
		return nil
	}
	if active.RunID == "" || active.UserID != next.UserID || active.ConversationID != next.ConversationID ||
		active.Seq >= next.Seq || active.Route != routeNewRun {
		return ErrRoutingPending
	}
	var handoff recoveryCheckpoint
	if err := json.Unmarshal(snapshot, &handoff); err != nil {
		return err
	}
	if (handoff.Run.Replaces != active.RunID || handoff.Recovery == nil || handoff.Recovery.Superseded != active.RunID) &&
		!observedSupersession(active) {
		// A new round is not a replacement. Its predecessor still owns
		// the unread final result and must complete its save first.
		return ErrRoutingPending
	}
	_, err := tx.Exec(ctx, `UPDATE shuttle_tasks SET state='closed',fence=fence+1,
	 lease_owner='',lease_until=NULL,version=version+1 WHERE id=$1`, active.ID)
	return err
}

func observedSupersession(active sqlc.ShuttleTask) bool {
	var saved recoveryCheckpoint
	return json.Unmarshal(active.Snapshot, &saved) == nil && saved.Run.ID == active.RunID &&
		saved.Run.Status == "cancelled" && saved.Run.Reason == orderReviewSuperseded
}

func recoveryAuthorizationUnknown(active sqlc.ShuttleTask) bool {
	if active.ID == uuid.Nil {
		return false
	}
	var saved recoveryCheckpoint
	return active.State == "auth_unknown" || json.Unmarshal(active.Snapshot, &saved) != nil || saved.Reauthorizing
}
