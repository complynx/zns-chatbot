package db_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/migrations"
)

func TestTaskMetricsGroupUnknownStatesAndPreserveRowsAcrossIndexRollback(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	rows, err := s.TaskMetrics(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
	states := []string{
		"accepted", "creating_run", "running", "awaiting_service", "awaiting_authorization",
		"auth_reauthorizing", "awaiting_user", "auth_unknown", "delivery_unknown",
		"private-user-canary", "future-state",
		"completed", "failed", "cancelled", "delivery_failed", "closed",
	}
	var original db.Task
	for index, state := range states {
		task := accepted(t, s, int64(index+1))
		if index == 0 {
			original = task
		}
		const acceptedAge = 90 * time.Second
		age := acceptedAge
		if state == "creating_run" {
			age = -time.Minute
		}
		_, err = s.Pool().Exec(ctx,
			"UPDATE shuttle_tasks SET state=$2,created_at=statement_timestamp()-$3::double precision*interval '1 second' WHERE id=$1",
			task.ID, state, age.Seconds(),
		)
		require.NoError(t, err)
	}
	rows, err = s.TaskMetrics(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 10)
	for _, row := range rows {
		if row.State == "other" {
			require.EqualValues(t, 2, row.Count)
		} else {
			require.Contains(t, states[:9], row.State)
			require.EqualValues(t, 1, row.Count)
		}
		if row.State == "creating_run" {
			require.Negative(t, row.AcceptedAgeSeconds)
		} else {
			require.Greater(t, row.AcceptedAgeSeconds, time.Minute.Seconds())
		}
	}
	scope := db.EventScope{BotID: 1, Organization: "org"}
	start := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	change := db.EventChange{ID: uuid.New(), Document: events.Document{
		Name: "Metrics rollback fixture", Timezone: "Europe/Minsk", StartsAt: start, EndsAt: start.Add(time.Hour),
	}}
	event, err := s.ChangeEvent(ctx, scope, "operator", "metrics-replay", change)
	require.NoError(t, err)
	var before string
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM shuttle_tasks t",
	).Scan(&before))
	driver := stdlib.OpenDBFromPool(s.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	for _, phase := range []struct {
		name   string
		apply  func() error
		exists bool
	}{
		{"down", func() error { _, downErr := provider.ApplyVersion(ctx, 26, false); return downErr }, false},
		{"up", func() error { _, upErr := provider.ApplyVersion(ctx, 26, true); return upErr }, true},
	} {
		require.NoError(t, phase.apply(), phase.name)
		var after string
		require.NoError(t, s.Pool().QueryRow(ctx,
			"SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM shuttle_tasks t",
		).Scan(&after))
		require.Equal(t, before, after, phase.name)
		var exists bool
		require.NoError(t, s.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND indexname='shuttle_tasks_unresolved_metrics_idx')",
		).
			Scan(&exists))
		require.Equal(t, phase.exists, exists, phase.name)
		replay, replayErr := s.ChangeEvent(ctx, scope, "operator", "metrics-replay", change)
		require.NoError(t, replayErr)
		require.Equal(t, event, replay)
		duplicate, reused, intakeErr := s.Intake(ctx, db.IntakeRequest{
			BotID: 1, UpdateID: 1, TelegramUserID: 2, ChatID: 3,
			Payload: json.RawMessage(`{"kind":"message","text":"changed retry"}`),
		}, nil)
		require.NoError(t, intakeErr)
		require.True(t, reused)
		require.Equal(t, original.ID, duplicate.ID)
		require.JSONEq(t, string(original.Request), string(duplicate.Request))
		current, readErr := s.TaskMetrics(ctx)
		require.NoError(t, readErr)
		require.Len(t, current, len(rows))
	}
}
