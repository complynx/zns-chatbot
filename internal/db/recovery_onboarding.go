package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Carry local onboarding work in the same transaction as observer ownership.
// The immutable source request still owns the deterministic continuation key.
func carryRecoveryOnboarding(ctx context.Context, tx pgx.Tx, next Task,
	snapshot json.RawMessage) (json.RawMessage, error) {
	var current map[string]json.RawMessage
	if err := json.Unmarshal(snapshot, &current); err != nil {
		return nil, err
	}
	var handoff recoveryCheckpoint
	if err := json.Unmarshal(snapshot, &handoff); err != nil {
		return nil, err
	}
	previous := handoff.Run.Replaces
	if previous == "" && handoff.Recovery != nil {
		previous = handoff.Recovery.Observed
	}
	if previous == "" {
		return snapshot, nil
	}
	var source Task
	err := tx.QueryRow(ctx, `SELECT id,request,snapshot,state FROM shuttle_tasks
	 WHERE user_id=$1 AND conversation_id=$2 AND run_id=$3 AND route='new_run'
	 AND id<>$4 ORDER BY seq DESC LIMIT 1`, next.UserID, next.ConversationID, previous, next.ID).
		Scan(&source.ID, &source.Request, &source.Snapshot, &source.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return nil, err
	}
	if source.State == taskStateCancelled {
		return snapshot, nil
	}
	var saved recoveryCheckpoint
	if err = json.Unmarshal(source.Snapshot, &saved); err != nil {
		return nil, err
	}
	if source.State == taskStateFailed && saved.Run.Status == taskStateFailed {
		return snapshot, nil
	}
	origins := saved.Origins
	rows, err := tx.Query(ctx, `SELECT id FROM shuttle_tasks WHERE user_id=$1 AND conversation_id=$2
	 AND (id=$3 OR target_task_id=$3) AND id<>$4 AND seq<=$5
	 AND (id=$3 OR state NOT IN ('failed','cancelled','closed'))
	 AND COALESCE(request->>'start_continuation','')<>'' ORDER BY seq`,
		next.UserID, next.ConversationID, source.ID, next.ID, next.Seq)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	origins = append(origins, ids...)
	if len(origins) > 0 {
		current["recovery_start_origins"], err = json.Marshal(origins)
		if err != nil {
			return nil, err
		}
	}
	if saved.Welcome && source.State != taskStateCompleted {
		current["welcome_after_onboarding"] = json.RawMessage(`true`)
	}
	return json.Marshal(current)
}
