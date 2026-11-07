package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestStartContinuationQueueFailureRollsBackAndReplayKeepsOneChild(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	parent := accepted(t, s, 1)
	parent, err := s.Route(t.Context(), parent.ID)
	require.NoError(t, err)
	parent, err = s.Claim(t.Context(), parent.ID, "test", time.Minute)
	require.NoError(t, err)
	parent.State = "completed"
	parent, err = s.Save(t.Context(), parent, "test", parent.Fence, nil)
	require.NoError(t, err)
	payload := json.RawMessage(`{"kind":"structured","text":"/profile","structured_command":"/profile"}`)
	queueError := errors.New("queue unavailable")
	_, err = s.IntakeStartContinuation(t.Context(), parent.ID, payload,
		func(context.Context, pgx.Tx, uuid.UUID) error { return queueError })
	require.ErrorIs(t, err, queueError)
	var count int
	require.NoError(t, s.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&count))
	require.Equal(t, 1, count)
	queued := 0
	enqueue := func(context.Context, pgx.Tx, uuid.UUID) error { queued++; return nil }
	child, err := s.IntakeStartContinuation(t.Context(), parent.ID, payload, enqueue)
	require.NoError(t, err)
	replayed, err := s.IntakeStartContinuation(t.Context(), parent.ID, payload, enqueue)
	require.NoError(t, err)
	require.Equal(t, child.ID, replayed.ID)
	require.Equal(t, parent.Seq+1, child.Seq)
	require.Equal(t, 1, queued)
	require.JSONEq(t, string(payload), string(child.Request))
	require.Equal(t, parent.ID, accepted(t, s, 1).ID)
	require.NoError(t, s.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_inbox").Scan(&count))
	require.Equal(t, 1, count, "Internal follow-up must not invent a Telegram update")
}
