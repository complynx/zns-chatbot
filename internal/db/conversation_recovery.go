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
	), COALESCE((SELECT max(seq) FROM shuttle_tasks WHERE conversation_id=$1),0)`,
		t.ConversationID, t.Seq).Scan(&ready, &seq)
	return ready, seq, err
}

// RecoveryRunSession retains the original authorization receipt after a lost local pointer.
func (s *Store) RecoveryRunSession(ctx context.Context, user uuid.UUID, run, session string) (json.RawMessage, error) {
	var snapshot json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT snapshot FROM shuttle_tasks WHERE user_id=$1 AND
	 (run_id=$2 OR ($3<>'' AND auth_session_id=$3))
	 ORDER BY COALESCE((snapshot->'session'->>'version')::bigint,0) DESC,
	 (run_id=$2) DESC,seq DESC LIMIT 1`, user, run, session).Scan(&snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return snapshot, err
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
	var observed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_tasks WHERE conversation_id=$1
	 AND run_id=$2 AND id<>$3 AND route='new_run'
	 AND (state IN ('completed','cancelled') OR
	 (state IN ('failed','delivery_unknown','delivery_failed') AND
	 snapshot#>>'{run,status}' IN ('completed','succeeded','failed','cancelled','canceled'))))`,
		t.ConversationID, t.RunID, t.ID).Scan(&observed)
	if err != nil {
		return t, false, err
	}
	if observed {
		return t, false, nil
	}
	active, err := activeTask(ctx, q, c.ActiveTaskID)
	if err != nil {
		return t, false, err
	}
	if active.ID != uuid.Nil && active.ID != t.ID {
		if active.RunID == t.RunID {
			return t, false, nil
		}
		if active.ID != t.TargetTaskID {
			return t, false, ErrRoutingPending
		}
		if _, err = tx.Exec(ctx, `UPDATE shuttle_tasks SET state='closed',fence=fence+1,
		 lease_owner='',lease_until=NULL,version=version+1 WHERE id=$1`, active.ID); err != nil {
			return t, false, err
		}
	}
	result, err := tx.Exec(ctx, `UPDATE shuttle_tasks SET route='new_run',target_task_id=$4,
	 target_run_id='',run_id=$5,auth_session_id=$6,snapshot=$7,state='awaiting_service',version=version+1
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
