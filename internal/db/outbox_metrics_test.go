package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOutboxMetricsCountChunksIndependentlyOfTaskAndPreserveRowsAcrossBaselineReplay(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	rows, err := s.OutboxMetrics(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
	task := accepted(t, s, 1)
	_, err = s.Pool().Exec(ctx, "UPDATE shuttle_tasks SET state='closed' WHERE id=$1", task.ID)
	require.NoError(t, err)
	states := []string{"pending", "pending", "retryable", "send_inflight", "delivery_unknown",
		"private-user-canary", "future-state", "delivered", "delivery_blocked", "delivery_failed"}
	var first uuid.UUID
	for index, state := range states {
		id := uuid.New()
		if index == 0 {
			first = id
		}
		age := 90 * time.Second
		if state == "retryable" {
			age = -time.Minute
		}
		_, err = s.Pool().Exec(ctx, "INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload,state,created_at)\n"+
			"\t\t VALUES($1,$2,$3,$4,'{\"private-payload\":\"not a label\"}',$5,"+
			"statement_timestamp()-$6::double precision*interval '1 second')",
			id, task.ID, id.String(), index, state, age.Seconds())
		require.NoError(t, err)
	}
	rows, err = s.OutboxMetrics(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for _, row := range rows {
		if row.State == "other" || row.State == "pending" {
			require.EqualValues(t, 2, row.Count)
		} else {
			require.Contains(t, states[2:5], row.State)
			require.EqualValues(t, 1, row.Count)
		}
		if row.State == "retryable" {
			require.Negative(t, row.CreatedAgeSeconds)
		} else {
			require.Greater(t, row.CreatedAgeSeconds, time.Minute.Seconds())
		}
	}
	var before string
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM shuttle_outbox t").Scan(&before))
	require.NoError(t, s.Migrate(ctx))
	var exists bool
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND "+
			"indexname='shuttle_outbox_unresolved_metrics_idx')").
		Scan(&exists))
	require.True(t, exists)
	_, err = s.Pool().Exec(
		ctx,
		`INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload)
		 VALUES($1,$2,$3,100,'{"changed retry":true}') ON CONFLICT(dedup_key) DO NOTHING`,
		uuid.New(),
		task.ID,
		first.String(),
	)
	require.NoError(t, err)
	var after string
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM shuttle_outbox t").Scan(&after))
	require.Equal(t, before, after)
	current, readErr := s.OutboxMetrics(ctx)
	require.NoError(t, readErr)
	require.Len(t, current, len(rows))
}
