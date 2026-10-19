package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/observability"
	"github.com/complynx/zns-chatbot/internal/task"
)

func TestQueueMetricsUseDueDeadlinesAndOwnedKindsInConfiguredSchema(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	h.stop(t)
	ctx := context.Background()
	for _, kind := range []string{"shuttle_task", "zinusya_payment_notification", "zinusya_pair_notification", "zinusya_order_notification"} {
		for _, state := range []string{"available", "scheduled", "retryable"} {
			_, err := h.store.Pool().
				Exec(ctx, `INSERT INTO river_job(kind,state,max_attempts,args,created_at,scheduled_at)
                VALUES ($1,$2,25,'{}',now()-interval '1 year',now()-interval '2 minutes'),
                       ($1,$2,25,'{}',now()-interval '1 year',now()+interval '1 hour')`, kind, state)
			require.NoError(t, err)
		}
	}
	_, err := h.store.Pool().
		Exec(ctx, `INSERT INTO river_job(kind,state,queue,max_attempts,args,scheduled_at,finalized_at)
        SELECT 'shuttle_task',state,'default',25,'{}',now()-interval '1 year',
               CASE WHEN state IN ('completed','cancelled','discarded') THEN now() ELSE NULL END
        FROM unnest(ARRAY['running','completed','cancelled','discarded','pending']::river_job_state[]) state;
        INSERT INTO river_job(kind,queue,max_attempts,args,scheduled_at) VALUES
          ('private-user-token','default',25,'{}',now()-interval '1 year'),
          ('shuttle_task','private-queue',25,'{}',now()-interval '1 year')`)
	require.NoError(t, err)
	rows, err := task.QueueMetrics(ctx, h.store.Pool())
	require.NoError(t, err)
	require.Len(t, rows, 12)
	for _, row := range rows {
		require.Equal(t, int64(1), row.Count)
		require.InDelta(t, 120, row.ScheduledAgeSeconds, 5)
	}
	telemetry, err := observability.New(ctx, observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	require.NoError(t, telemetry.ObserveQueue(func(query context.Context) ([]observability.QueueMetric, error) {
		return task.QueueMetrics(query, h.store.Pool())
	}))
	response := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `zinusya_queue_due{kind="shuttle_task",state="scheduled"} 1`)
	require.Contains(
		t,
		response.Body.String(),
		`zinusya_queue_due{kind="zinusya_order_notification",state="scheduled"} 1`,
	)
	require.NotContains(t, response.Body.String(), "private-user-token")
	require.NotContains(t, response.Body.String(), "private-queue")
}

func TestQueueMetricsBlockedQueryKeepsTaskAndPoolMetricsHealthy(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	h.stop(t)
	ctx := context.Background()
	lock, err := h.store.Pool().Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Rollback(ctx) })
	_, err = lock.Exec(ctx, "LOCK TABLE river_job IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	telemetry, err := observability.New(ctx, observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	require.NoError(t, telemetry.ObserveDatabasePool(h.store.Pool()))
	require.NoError(t, telemetry.ObserveTasks(h.store))
	require.NoError(t, telemetry.ObserveQueue(func(query context.Context) ([]observability.QueueMetric, error) {
		return task.QueueMetrics(query, h.store.Pool())
	}))
	response := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "\nzinusya_queue_collection_success 0\n")
	require.NotContains(t, response.Body.String(), "\nzinusya_queue_due{")
	require.Contains(t, response.Body.String(), "\nzinusya_tasks_collection_success 1\n")
	require.Contains(t, response.Body.String(), "\nzinusya_db_pool_max ")
	require.NoError(t, lock.Rollback(ctx))
}

type queueMetricWorker struct {
	river.WorkerDefaults[task.JobArgs]

	blocked, snooze  uuid.UUID
	entered, release chan struct{}
}

func (w *queueMetricWorker) Work(ctx context.Context, job *river.Job[task.JobArgs]) error {
	if job.Args.TaskID == w.blocked {
		select {
		case w.entered <- struct{}{}:
		default:
		}
		select {
		case <-w.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if job.Args.TaskID == w.snooze {
		return river.JobSnooze(time.Hour)
	}
	return nil
}

func TestQueueMetricsFollowRealWorkerBacklogDrainAndSnooze(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, nil)
	ctx := context.Background()
	w := &queueMetricWorker{blocked: uuid.New(), snooze: uuid.New(), entered: make(chan struct{}, 1),
		release: make(chan struct{}, 1)}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	client, err := river.NewClient(riverpgxv5.New(h.store.Pool()), &river.Config{
		Schema: h.store.Pool().Config().ConnConfig.RuntimeParams["search_path"], Workers: workers,
		Queues:   map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
		Logger:   slog.New(slog.DiscardHandler),
		TestOnly: true,
	})
	require.NoError(t, err)
	require.NoError(t, client.Start(ctx))
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, client.StopAndCancel(stop))
	})
	_, err = client.Insert(ctx, task.JobArgs{TaskID: w.blocked}, nil)
	require.NoError(t, err)
	select {
	case <-w.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("fixture worker did not start")
	}
	queued, err := client.Insert(ctx, task.JobArgs{TaskID: uuid.New()},
		&river.InsertOpts{ScheduledAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)
	require.Equal(t, "scheduled", string(queued.Job.State))
	require.Eventually(t, func() bool {
		rows, readErr := task.QueueMetrics(ctx, h.store.Pool())
		return readErr == nil && len(rows) == 1 && rows[0].State == "available" && rows[0].Count == 1
	}, 10*time.Second, 50*time.Millisecond, "River must promote the overdue job while the worker slot is occupied")
	rows, err := task.QueueMetrics(ctx, h.store.Pool())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "shuttle_task", rows[0].Kind)
	require.Equal(t, "available", rows[0].State)
	require.Equal(t, int64(1), rows[0].Count)
	require.GreaterOrEqual(t, rows[0].ScheduledAgeSeconds, float64(59))
	w.release <- struct{}{}
	require.Eventually(t, func() bool {
		job, readErr := client.JobGet(ctx, queued.Job.ID)
		return readErr == nil && job.State == "completed"
	}, 10*time.Second, 20*time.Millisecond)
	snoozed, err := client.Insert(ctx, task.JobArgs{TaskID: w.snooze}, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		job, readErr := client.JobGet(ctx, snoozed.Job.ID)
		return readErr == nil && job.State == "scheduled" && time.Until(job.ScheduledAt) > 59*time.Minute
	}, 10*time.Second, 20*time.Millisecond)
	rows, err = task.QueueMetrics(ctx, h.store.Pool())
	require.NoError(t, err)
	require.Empty(t, rows, "a snoozed job is not overdue merely because it already ran")
	var jobs int
	require.NoError(t, h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&jobs))
	require.Equal(t, 3, jobs, "draining metrics must not delete retained River history")
}
