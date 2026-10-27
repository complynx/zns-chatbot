package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

type ReactionCursor struct {
	UserID, TaskID uuid.UUID
	LoomChatID     string
	After          int64
}

const reactionPending = "pending"
const reactionFailed = "failed"

// ErrReactionTargetPending retains the projection until its owned delivery is acknowledged.
var ErrReactionTargetPending = errors.New("reaction target has no confirmed Telegram receipt yet")

type ReactionProjection struct {
	MessageSeq, Watermark, ChatID, MessageID int64
	Emoji                                    string
	Kind, RunID, EventID                     string
}

type ReactionDelivery struct {
	ReactionProjection

	UserID, AttemptID uuid.UUID
	LoomChatID        string
	Version           int64
}

func (s *Store) RegisterReactionChat(ctx context.Context, user, task uuid.UUID, chat string) error {
	result, err := s.pool.Exec(ctx, `INSERT INTO shuttle_reaction_cursors(user_id,loom_chat_id,task_id)
SELECT $1,$2,$3 FROM shuttle_tasks t WHERE t.id=$3 AND t.user_id=$1
ON CONFLICT(user_id,loom_chat_id) DO UPDATE SET
task_id=EXCLUDED.task_id,active=true`, user, chat, task)
	if err == nil && result.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

func (s *Store) DueReactionChats(ctx context.Context, bot int64) ([]ReactionCursor, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.user_id,c.task_id,c.loom_chat_id,c.after_seq
FROM shuttle_reaction_cursors c JOIN shuttle_users u ON u.id=c.user_id
WHERE u.bot_id=$1 AND c.active AND c.next_check_at<=now() ORDER BY c.next_check_at LIMIT 5`, bot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ReactionCursor
	for rows.Next() {
		var c ReactionCursor
		if err = rows.Scan(&c.UserID, &c.TaskID, &c.LoomChatID, &c.After); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// ReactionPage commits desired state and its history cursor together. Snapshot
// watermarks protect against late deltas and replay of an older history page.
func (s *Store) ReactionPage(ctx context.Context, c ReactionCursor, after int64, empty bool,
	items []ReactionProjection,
) error {
	if after < c.After {
		return ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current int64
	err = tx.QueryRow(ctx, `SELECT after_seq FROM shuttle_reaction_cursors
WHERE user_id=$1 AND loom_chat_id=$2 FOR UPDATE`, c.UserID, c.LoomChatID).Scan(&current)
	if err != nil {
		return err
	}
	if current != c.After {
		return ErrConflict
	}
	for _, item := range items {
		if !telegram.SupportedReaction(item.Emoji) || item.Watermark < item.MessageSeq {
			return ErrConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO shuttle_telegram_reactions
(user_id,loom_chat_id,message_seq,chat_id,message_id,watermark,emoji,target_kind,target_run_id,target_event_id)
SELECT $1,$2,$3,NULLIF($4,0),NULLIF($5,0),$6,$7,$8,$9,$10 WHERE $7<>'' OR EXISTS(
SELECT 1 FROM shuttle_telegram_reactions
 WHERE user_id=$1 AND loom_chat_id=$2 AND message_seq=$3)
ON CONFLICT(user_id,loom_chat_id,message_seq) DO UPDATE SET
watermark=EXCLUDED.watermark,emoji=EXCLUDED.emoji,version=shuttle_telegram_reactions.version+1,
state=CASE WHEN shuttle_telegram_reactions.state IN('unknown','sending')
 THEN shuttle_telegram_reactions.state ELSE 'pending' END,next_attempt_at=now(),updated_at=now()
WHERE EXCLUDED.watermark>shuttle_telegram_reactions.watermark
AND (EXCLUDED.chat_id IS NULL OR shuttle_telegram_reactions.chat_id IS NULL OR
 (EXCLUDED.chat_id=shuttle_telegram_reactions.chat_id AND EXCLUDED.message_id=shuttle_telegram_reactions.message_id))`,
			c.UserID, c.LoomChatID, item.MessageSeq, item.ChatID, item.MessageID, item.Watermark, item.Emoji,
			item.Kind, item.RunID, item.EventID)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE shuttle_reaction_cursors SET after_seq=$3,next_check_at=now()+interval '2 seconds',
active=CASE WHEN $4 AND task_id=$5 THEN EXISTS(SELECT 1 FROM shuttle_tasks t WHERE t.id=$5
 AND t.state NOT IN('completed','failed','cancelled','closed','auth_unknown','delivery_failed','delivery_unknown'))
 ELSE active END
WHERE user_id=$1 AND loom_chat_id=$2`, c.UserID, c.LoomChatID, after, empty, c.TaskID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DelayReactionChat(ctx context.Context, c ReactionCursor) error {
	_, err := s.pool.Exec(ctx, `UPDATE shuttle_reaction_cursors SET next_check_at=now()+interval '5 seconds'
WHERE user_id=$1 AND loom_chat_id=$2`, c.UserID, c.LoomChatID)
	return err
}

// RecoverUnknownReactions freezes uncertain targets; normal messages remain independent.
func (s *Store) RecoverUnknownReactions(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, "UPDATE shuttle_telegram_reactions SET state='unknown' WHERE state='sending'")
	return err
}

func (s *Store) BeginReaction(ctx context.Context, bot int64) (ReactionDelivery, error) {
	var d ReactionDelivery
	d.AttemptID = uuid.New()
	err := s.pool.QueryRow(ctx, `WITH candidate AS (
 SELECT r.user_id,r.loom_chat_id,r.message_seq FROM shuttle_telegram_reactions r
 JOIN shuttle_users u ON u.id=r.user_id WHERE u.bot_id=$1 AND r.state='pending' AND r.next_attempt_at<=now()
 AND r.chat_id IS NOT NULL AND r.message_id IS NOT NULL
 ORDER BY r.next_attempt_at LIMIT 1 FOR UPDATE OF r SKIP LOCKED)
UPDATE shuttle_telegram_reactions r SET state='sending',attempt_id=$2,
attempt_emoji=r.emoji,attempt_version=r.version,attempt_watermark=r.watermark FROM candidate c
WHERE r.user_id=c.user_id AND r.loom_chat_id=c.loom_chat_id AND r.message_seq=c.message_seq
RETURNING r.user_id,r.loom_chat_id,r.message_seq,r.watermark,r.chat_id,r.message_id,r.emoji,r.version`,
		bot, d.AttemptID).Scan(&d.UserID, &d.LoomChatID, &d.MessageSeq, &d.Watermark, &d.ChatID,
		&d.MessageID, &d.Emoji, &d.Version)
	return d, err
}

func (s *Store) FinishReaction(ctx context.Context, d ReactionDelivery, state string, delay time.Duration) error {
	if state != "delivered" && state != reactionFailed && state != reactionPending && state != "unknown" {
		return ErrConflict
	}
	_, err := s.pool.Exec(ctx, `UPDATE shuttle_telegram_reactions SET
state=CASE WHEN $6='unknown' THEN 'unknown' WHEN version<>$5 THEN 'pending' ELSE $6 END,
next_attempt_at=now()+$7::interval,updated_at=now()
WHERE user_id=$1 AND loom_chat_id=$2 AND message_seq=$3 AND attempt_id=$4 AND state='sending'`,
		d.UserID, d.LoomChatID, d.MessageSeq, d.AttemptID, d.Version, state, delay.String())
	return err
}

// ReactionTarget resolves protocol identities through durable intake/delivery receipts, never text matching.
func (s *Store) ReactionTarget(ctx context.Context, user uuid.UUID, kind, run, id string) (int64, int64, error) {
	var chat, message int64
	switch kind {
	case "message.user", "input.steering", "input.answer":
		err := s.pool.QueryRow(ctx, `SELECT (request->>'chat_id')::bigint,
(request->'raw_update'->'message'->>'message_id')::bigint FROM shuttle_tasks
WHERE user_id=$1 AND ((route='new_run' AND run_id=$2 AND $3='message.user') OR
 (snapshot->'input_receipt'->>'input_id'=$4 AND $3 IN('input.steering','input.answer')))
AND request->'raw_update'->'message'->>'message_id' IS NOT NULL ORDER BY seq LIMIT 1`,
			user, run, kind, id).Scan(&chat, &message)
		return chat, message, err
	case "message.assistant", "question.created", "approval.created":
		err := s.pool.QueryRow(ctx, `SELECT (o.receipt->>'chat_id')::bigint,(o.receipt->>'message_id')::bigint
FROM shuttle_outbox o JOIN shuttle_tasks t ON t.id=o.task_id
WHERE t.user_id=$1 AND t.run_id=$2 AND o.state='delivered' AND
(($3='message.assistant' AND o.dedup_key LIKE t.id::text||':result:%') OR
 ($3='question.created' AND o.payload->'_loom_context'->>'question_id'=$4) OR
 ($3='approval.created' AND o.payload->'_loom_context'->>'approval_id'=$4))
ORDER BY t.seq,o.chunk_seq LIMIT 1`, user, run, kind, id).Scan(&chat, &message)
		if !errors.Is(err, pgx.ErrNoRows) {
			return chat, message, err
		}
		var pending bool
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_tasks t
WHERE t.user_id=$1 AND t.run_id=$2 AND
(t.state NOT IN('completed','failed','cancelled','closed','auth_unknown','delivery_failed','delivery_unknown') OR
 EXISTS(SELECT 1 FROM shuttle_outbox o WHERE o.task_id=t.id
 AND o.state IN('pending','retryable','send_inflight','delivery_unknown') AND
 (($3='message.assistant' AND o.dedup_key LIKE t.id::text||':result:%') OR
  ($3='question.created' AND o.payload->'_loom_context'->>'question_id'=$4) OR
  ($3='approval.created' AND o.payload->'_loom_context'->>'approval_id'=$4)))))`,
			user, run, kind, id).Scan(&pending)
		if err != nil {
			return 0, 0, err
		}
		if pending {
			return 0, 0, ErrReactionTargetPending
		}
		return 0, 0, pgx.ErrNoRows
	default:
		return 0, 0, pgx.ErrNoRows
	}
}

// ResolveReactionTargets retries only identity mapping, never an uncertain Telegram effect.
// Deferred targets are independent, so one unknown message delivery cannot hold the chat cursor.
func (s *Store) ResolveReactionTargets(ctx context.Context, bot int64) error {
	rows, err := s.pool.Query(ctx, `SELECT r.user_id,r.loom_chat_id,r.message_seq,
r.target_kind,r.target_run_id,r.target_event_id FROM shuttle_telegram_reactions r
JOIN shuttle_users u ON u.id=r.user_id WHERE u.bot_id=$1 AND r.state='pending'
AND r.chat_id IS NULL AND r.next_attempt_at<=now() ORDER BY r.next_attempt_at LIMIT 5`, bot)
	if err != nil {
		return err
	}
	type target struct {
		user                uuid.UUID
		chat, kind, run, id string
		seq                 int64
	}
	var targets []target
	for rows.Next() {
		var v target
		if err = rows.Scan(&v.user, &v.chat, &v.seq, &v.kind, &v.run, &v.id); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range targets {
		chat, message, mapErr := s.ReactionTarget(ctx, v.user, v.kind, v.run, v.id)
		state := reactionPending
		if errors.Is(mapErr, pgx.ErrNoRows) {
			state = reactionFailed
		} else if mapErr != nil &&
			!errors.Is(mapErr, ErrReactionTargetPending) {
			return mapErr
		}
		_, err = s.pool.Exec(ctx, `UPDATE shuttle_telegram_reactions SET chat_id=NULLIF($4,0),message_id=NULLIF($5,0),
state=$6,next_attempt_at=now()+interval '2 seconds' WHERE user_id=$1 AND loom_chat_id=$2
AND message_seq=$3 AND chat_id IS NULL AND state='pending'`, v.user, v.chat, v.seq, chat, message, state)
		if err != nil {
			return err
		}
	}
	return nil
}
