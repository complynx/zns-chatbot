package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// HasDeferredStartForTask finds a later /start request routed into this same
// onboarding conversation, including an input whose receipt is still pending.
func (s *Store) HasDeferredStartForTask(ctx context.Context, taskID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM shuttle_tasks parent JOIN shuttle_tasks child
		ON child.target_task_id=parent.id AND child.user_id=parent.user_id
		AND child.conversation_id=parent.conversation_id
		WHERE parent.id=$1 AND child.route='input'
		AND child.state NOT IN ('failed','cancelled','closed')
		AND coalesce(child.request->>'start_continuation','')<>'')`, taskID).Scan(&exists)
	return exists, err
}

// IntakeStartContinuation accepts one internal follow-up without inventing a
// Telegram update. The parent request retains its original inbox identity.
func (s *Store) IntakeStartContinuation(ctx context.Context, parentID uuid.UUID, payload json.RawMessage,
	enqueue func(context.Context, pgx.Tx, uuid.UUID) error,
) (Task, error) {
	if !json.Valid(payload) {
		return Task{}, errors.New("invalid start continuation")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	parent, err := q.GetTask(ctx, parentID)
	if err != nil {
		return Task{}, err
	}
	if _, err = q.LockConversation(ctx, parent.ConversationID); err != nil {
		return Task{}, err
	}
	parent, err = q.GetTask(ctx, parentID)
	if err != nil {
		return Task{}, err
	}
	id := uuid.NewSHA1(parentID, []byte("start-continuation"))
	existing, err := q.GetTask(ctx, id)
	if err == nil {
		return task(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Task{}, err
	}
	if parent.State != taskStateCompleted {
		return Task{}, ErrConflict
	}
	seq, err := q.NextSequence(ctx, parent.ConversationID)
	if err != nil {
		return Task{}, err
	}
	child, err := q.InsertTask(ctx, sqlc.InsertTaskParams{ID: id, UserID: parent.UserID,
		ConversationID: parent.ConversationID, Seq: seq, Request: payload})
	if err != nil {
		return Task{}, err
	}
	if enqueue != nil {
		if err = enqueue(ctx, tx, id); err != nil {
			return Task{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return task(child), nil
}
