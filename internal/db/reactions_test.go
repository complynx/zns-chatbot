package db_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestReactionWatermarkAndUnknownDeliveryPreserveNewestDesiredState(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 1)
	c := db.ReactionCursor{UserID: task.UserID, TaskID: task.ID, LoomChatID: "chat"}
	ctx := t.Context()
	require.NoError(t, s.RegisterReactionChat(ctx, c.UserID, c.TaskID, c.LoomChatID))
	item := db.ReactionProjection{MessageSeq: 1, Watermark: 5, ChatID: 2, MessageID: 3, Emoji: "👍"}
	require.NoError(t, s.ReactionPage(ctx, c, 1, false, []db.ReactionProjection{item}))
	d, err := s.BeginReaction(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "👍", d.Emoji)
	c.After = 1
	item.Emoji, item.Watermark = "🙈", 8
	require.NoError(t, s.ReactionPage(ctx, c, 2, false, []db.ReactionProjection{item}))
	require.NoError(t, s.FinishReaction(ctx, d, "unknown", 0))
	_, err = s.BeginReaction(ctx, 1)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var state, emoji string
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT state,emoji FROM shuttle_telegram_reactions").Scan(&state, &emoji))
	require.Equal(t, "unknown", state)
	require.Equal(t, "🙈", emoji)
	var attemptedEmoji string
	var attemptedVersion, attemptedWatermark int64
	require.NoError(t, s.Pool().QueryRow(ctx, `SELECT attempt_emoji,attempt_version,attempt_watermark
FROM shuttle_telegram_reactions`).Scan(&attemptedEmoji, &attemptedVersion, &attemptedWatermark))
	require.Equal(t, d.Emoji, attemptedEmoji)
	require.Equal(t, d.Version, attemptedVersion)
	require.Equal(t, d.Watermark, attemptedWatermark)
	c.After = 2
	item.Watermark, item.Emoji = 6, "😢"
	require.NoError(t, s.ReactionPage(ctx, c, 3, false, []db.ReactionProjection{item}))
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT emoji FROM shuttle_telegram_reactions").Scan(&emoji))
	require.Equal(t, "🙈", emoji)
}

func TestDeferredReactionSurvivesCursorAdvanceAndDelayedReceipt(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 3)
	ctx := t.Context()
	_, err := s.Pool().Exec(ctx, "UPDATE shuttle_tasks SET run_id='late',state='running' WHERE id=$1", task.ID)
	require.NoError(t, err)
	c := db.ReactionCursor{UserID: task.UserID, TaskID: task.ID, LoomChatID: "chat"}
	require.NoError(t, s.RegisterReactionChat(ctx, c.UserID, c.TaskID, c.LoomChatID))
	_, _, err = s.ReactionTarget(ctx, task.UserID, "message.assistant", "late", "")
	require.ErrorIs(t, err, db.ErrReactionTargetPending)
	item := db.ReactionProjection{MessageSeq: 3, Watermark: 4, Emoji: "👍", Kind: "message.assistant", RunID: "late"}
	require.NoError(t, s.ReactionPage(ctx, c, 4, false, []db.ReactionProjection{item}))
	require.NoError(t, s.ResolveReactionTargets(ctx, 1))
	_, err = s.BeginReaction(ctx, 1)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// Another target is ready while this message still has no acknowledged delivery.
	c.After = 4
	ready := db.ReactionProjection{MessageSeq: 5, Watermark: 6, Emoji: "🙈", ChatID: 2, MessageID: 30}
	require.NoError(t, s.ReactionPage(ctx, c, 6, false, []db.ReactionProjection{ready}))
	d, err := s.BeginReaction(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 5, d.MessageSeq)
	require.NoError(t, s.FinishReaction(ctx, d, "delivered", 0))
	_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload,state,receipt)
VALUES($1,$2,$3,0,'{}','delivered','{"chat_id":2,"message_id":31}');`,
		uuid.New(), task.ID, task.ID.String()+":result:0")
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, "UPDATE shuttle_telegram_reactions SET next_attempt_at=now()")
	require.NoError(t, err)
	require.NoError(t, s.ResolveReactionTargets(ctx, 1))
	_, err = s.Pool().Exec(ctx, "UPDATE shuttle_telegram_reactions SET next_attempt_at=now()")
	require.NoError(t, err)
	d, err = s.BeginReaction(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 3, d.MessageSeq)
	require.EqualValues(t, 31, d.MessageID)
	require.NoError(t, s.FinishReaction(ctx, d, "delivered", 0))
	_, err = s.BeginReaction(ctx, 1)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestReactionRecoveryNeverReplaysAnInflightMutation(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 2)
	c := db.ReactionCursor{UserID: task.UserID, TaskID: task.ID, LoomChatID: "chat"}
	ctx := t.Context()
	require.NoError(t, s.RegisterReactionChat(ctx, c.UserID, c.TaskID, c.LoomChatID))
	item := db.ReactionProjection{MessageSeq: 1, Watermark: 2, ChatID: 2, MessageID: 3, Emoji: "👍"}
	require.NoError(t, s.ReactionPage(ctx, c, 2, false, []db.ReactionProjection{item}))
	_, err := s.BeginReaction(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, s.RecoverUnknownReactions(ctx))
	_, err = s.BeginReaction(ctx, 1)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = s.BeginReaction(ctx, 2)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
