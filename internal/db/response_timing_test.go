package db_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func responseTask(t *testing.T, s *db.Store) db.Task {
	t.Helper()
	ctx := t.Context()
	task := accepted(t, s, 1)
	task, err := s.Route(ctx, task.ID)
	require.NoError(t, err)
	task, err = s.Claim(ctx, task.ID, "response-test", time.Minute)
	require.NoError(t, err)
	task.RunID = "response-run"
	return task
}

func saveResponse(t *testing.T, s *db.Store, task db.Task, state string, keys ...string) db.Task {
	t.Helper()
	task.State = state
	out := make([]db.Outbound, len(keys))
	for n, key := range keys {
		out[n] = db.Outbound{DedupKey: key, Payload: json.RawMessage(`{"chat_id":3,"text":"reply"}`)}
	}
	saved, err := s.Save(t.Context(), task, "response-test", task.Fence, out)
	require.NoError(t, err)
	return saved
}

func responseReceipt(t *testing.T, s *db.Store, row db.Delivery, outcome string) db.DeliveryReceipt {
	t.Helper()
	reserved, err := s.ReserveDelivery(t.Context(), row.ID)
	require.NoError(t, err)
	finished, err := s.FinishDelivery(t.Context(), row.ID, reserved.AttemptID, outcome, nil)
	require.NoError(t, err)
	return finished
}

func TestResponseTimingWaitsForAllFinalChunksAndSurvivesStoreReopen(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := responseTask(t, s)
	_, err := s.Pool().
		Exec(t.Context(), "UPDATE shuttle_tasks SET response_started_at=clock_timestamp()-interval '2 hours' WHERE id=$1", task.ID)
	require.NoError(t, err)
	task = saveResponse(t, s, task, "awaiting_user", "prompt")
	rows, err := s.ListOutbox(t.Context(), task.ID)
	require.NoError(t, err)
	require.Nil(t, responseReceipt(t, s, rows[0], "delivered").ResponseElapsedSeconds)
	task = saveResponse(t, s, task, "completed", "final-first", "final-last")
	rows, err = s.ListOutbox(t.Context(), task.ID)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Nil(t, responseReceipt(t, s, rows[1], "delivered").ResponseElapsedSeconds)
	require.Nil(t, responseReceipt(t, s, rows[2], "retryable").ResponseElapsedSeconds)
	var before *time.Time
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", task.ID).
			Scan(&before),
	)
	require.Nil(t, before)
	reopened, err := db.Open(t.Context(), s.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	finished := responseReceipt(t, reopened, rows[2], "delivered")
	require.NotNil(t, finished.ResponseElapsedSeconds)
	require.GreaterOrEqual(t, *finished.ResponseElapsedSeconds, float64(2*time.Hour/time.Second))
	var delivered time.Time
	require.NoError(
		t,
		reopened.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", task.ID).
			Scan(&delivered),
	)
	repeated, err := reopened.FinishDelivery(t.Context(), rows[2].ID, finished.AttemptID, "delivered", nil)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Nil(t, repeated.ResponseElapsedSeconds)
	var unchanged time.Time
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", task.ID).
			Scan(&unchanged),
	)
	require.Equal(t, delivered, unchanged)
}

func TestResponseTimingExcludesUnconfirmedAndUninstrumentedReplies(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"historical", "control", "local", "failed-task", "empty", "unknown", "failed-send", "blocked", "missing-chunk"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			task := responseTask(t, s)
			switch mode {
			case "historical":
				_, err := s.Pool().
					Exec(t.Context(), "UPDATE shuttle_tasks SET response_started_at=NULL WHERE id=$1", task.ID)
				require.NoError(t, err)
			case "control":
				_, err := s.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET route='input' WHERE id=$1", task.ID)
				require.NoError(t, err)
			case "local":
				task.RunID = ""
			}
			state := "completed"
			if mode == "failed-task" {
				state = "failed"
			}
			keys := []string{"first", "last"}
			if mode == "empty" {
				keys = nil
			}
			task = saveResponse(t, s, task, state, keys...)
			rows, err := s.ListOutbox(t.Context(), task.ID)
			require.NoError(t, err)
			deliverExcludedResponse(t, s, rows, mode)
			var delivered *time.Time
			require.NoError(
				t,
				s.Pool().
					QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", task.ID).
					Scan(&delivered),
			)
			require.Nil(t, delivered)
		})
	}
}

func TestResponseTimingReceiptFailureLeavesNoCompletion(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := saveResponse(t, s, responseTask(t, s), "completed", "final")
	rows, err := s.ListOutbox(t.Context(), task.ID)
	require.NoError(t, err)
	reserved, err := s.ReserveDelivery(t.Context(), rows[0].ID)
	require.NoError(t, err)
	stale, err := s.FinishDelivery(t.Context(), rows[0].ID, uuid.New(), "delivered", nil)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Nil(t, stale.ResponseElapsedSeconds)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lost, err := s.FinishDelivery(ctx, rows[0].ID, reserved.AttemptID, "delivered", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, lost.ResponseElapsedSeconds)
	count, err := s.RecoverUnknownDeliveries(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	var delivered *time.Time
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", task.ID).
			Scan(&delivered),
	)
	require.Nil(t, delivered)
}

func TestResponseTimingMigrationRefusesToEraseAcceptedFacts(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	driver := stdlib.OpenDBFromPool(s.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(t.Context(), 32, false)
	require.NoError(t, err, "empty timing schema can be rolled back")
	_, err = provider.ApplyVersion(t.Context(), 32, true)
	require.NoError(t, err)
	task := accepted(t, s, 1)
	var before time.Time
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT response_started_at FROM shuttle_tasks WHERE id=$1", task.ID).
			Scan(&before),
	)
	_, err = provider.ApplyVersion(t.Context(), 32, false)
	require.ErrorContains(t, err, "response timing facts must be exported")
	var after time.Time
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT response_started_at FROM shuttle_tasks WHERE id=$1", task.ID).
			Scan(&after),
	)
	require.Equal(t, before, after)
	current, err := s.GetTask(t.Context(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task.Request, current.Request)
}

func TestResponseTimingRollbackPreservesConcurrentIntake(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	task := accepted(t, s, 1)
	_, err := s.Pool().Exec(ctx, "UPDATE shuttle_tasks SET response_started_at=NULL WHERE id=$1", task.ID)
	require.NoError(t, err)
	driver := stdlib.OpenDBFromPool(s.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	writer, err := s.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(context.Background()) }()
	_, err = writer.Exec(ctx, "LOCK TABLE shuttle_tasks IN ROW EXCLUSIVE MODE")
	require.NoError(t, err)
	var pid int32
	require.NoError(t, writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))
	result := make(chan error, 1)
	go func() {
		_, applyErr := provider.ApplyVersion(ctx, 32, false)
		result <- applyErr
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		e := s.Pool().QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM pg_locks WHERE mode='AccessExclusiveLock' AND NOT granted
AND $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked)
		return e == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	_, err = writer.Exec(ctx, "UPDATE shuttle_tasks SET response_started_at=clock_timestamp() WHERE id=$1", task.ID)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))
	require.ErrorContains(t, <-result, "response timing facts must be exported")
	var retained bool
	require.NoError(t, s.Pool().QueryRow(ctx,
		"SELECT response_started_at IS NOT NULL FROM shuttle_tasks WHERE id=$1", task.ID).Scan(&retained))
	require.True(t, retained)
}

func deliverExcludedResponse(t *testing.T, s *db.Store, rows []db.Delivery, mode string) {
	t.Helper()
	var err error
	if len(rows) > 0 {
		outcome := "delivered"
		switch mode {
		case "unknown":
			outcome = "delivery_unknown"
		case "failed-send":
			outcome = "delivery_failed"
		case "blocked":
			require.NoError(t, s.BlockPendingDelivery(t.Context(), rows[0].ID))
		case "missing-chunk":
			_, err = s.Pool().Exec(t.Context(), "DELETE FROM shuttle_outbox WHERE id=$1", rows[0].ID)
			require.NoError(t, err)
		}
		if mode != "blocked" && mode != "missing-chunk" {
			receipt := responseReceipt(t, s, rows[0], outcome)
			require.Nil(t, receipt.ResponseElapsedSeconds)
		}
		if outcome == "delivered" {
			receipt := responseReceipt(t, s, rows[1], "delivered")
			require.Nil(t, receipt.ResponseElapsedSeconds)
		}
	}
}
