// Package task owns durable inbox-to-Loom orchestration and Telegram delivery.
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"go.opentelemetry.io/otel/propagation"

	"github.com/complynx/shuttle/internal/db"
	"github.com/complynx/shuttle/internal/identity"
	"github.com/complynx/shuttle/internal/loom"
	"github.com/complynx/shuttle/internal/observability"
	"github.com/complynx/shuttle/internal/telegram"
)

type Options struct {
	BotID                                       int64
	PollInterval, AttemptTimeout, LeaseDuration time.Duration
	MaxWorkers                                  int
}
type Service struct {
	store       *db.Store
	iam         *identity.Client
	loom        *loom.Client
	tg          *telegram.Client
	observer    *observability.Telemetry
	logger      *slog.Logger
	options     Options
	queue       *river.Client[pgx.Tx]
	owner       string
	startMu     sync.Mutex
	lockConn    *pgxpool.Conn
	lockKey     string
	watchCancel context.CancelFunc
	watchDone   chan struct{}
	failures    chan error
}

// JobArgs deliberately contains no profile, credentials, or incoming content.
type JobArgs struct {
	TaskID uuid.UUID `json:"task_id"`
}

func (JobArgs) Kind() string { return "shuttle_task" }

type worker struct {
	river.WorkerDefaults[JobArgs]

	service *Service
}

func (w *worker) Work(ctx context.Context, job *river.Job[JobArgs]) error {
	err := w.service.work(ctx, job.Args.TaskID)
	outcome := "success"
	if err != nil {
		outcome = "retry"
	}
	w.service.record(ctx, "worker", outcome)
	return err
}

func queueSchema(pool *pgxpool.Pool) (string, error) {
	schema := strings.TrimSpace(pool.Config().ConnConfig.RuntimeParams["search_path"])
	if schema == "" {
		schema = "public"
	}
	// River requires an explicit single schema, which also keeps isolated tests isolated.
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(schema) {
		return "", errors.New("queue requires one explicit PostgreSQL schema")
	}
	return schema, nil
}
func MigrateQueue(ctx context.Context, pool *pgxpool.Pool) error {
	schema, err := queueSchema(pool)
	if err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: schema})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func New(
	store *db.Store,
	iam *identity.Client,
	loomClient *loom.Client,
	tg *telegram.Client,
	observer *observability.Telemetry,
	logger *slog.Logger,
	o Options,
) (*Service, error) {
	if store == nil || iam == nil || loomClient == nil || tg == nil || o.BotID <= 0 {
		return nil, errors.New("task dependencies and bot ID required")
	}
	if o.PollInterval == 0 {
		o.PollInterval = time.Second
	}
	if o.AttemptTimeout == 0 {
		o.AttemptTimeout = defaultAttemptTimeout
	}
	if o.LeaseDuration == 0 {
		o.LeaseDuration = o.AttemptTimeout + defaultLeaseMargin
	}
	if o.MaxWorkers == 0 {
		o.MaxWorkers = 8
	}
	if o.PollInterval <= 0 || o.AttemptTimeout <= 0 || o.LeaseDuration <= o.AttemptTimeout || o.MaxWorkers < 1 ||
		o.MaxWorkers > 128 {
		return nil, errors.New("invalid task worker limits")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Service{
		store:    store,
		iam:      iam,
		loom:     loomClient,
		tg:       tg,
		observer: observer,
		logger:   logger,
		options:  o,
		owner:    uuid.NewString(),
		failures: make(chan error, 1),
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &worker{service: s})
	schema, err := queueSchema(store.Pool())
	if err != nil {
		return nil, err
	}
	s.queue, err = river.NewClient(
		riverpgxv5.New(store.Pool()),
		&river.Config{
			Schema:     schema,
			Workers:    workers,
			Queues:     map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: o.MaxWorkers}},
			JobTimeout: o.AttemptTimeout + 2*time.Second,
			Logger:     logger,
		},
	)
	return s, err
}

// Start is called once after the previous application instance has ended.
func (s *Service) Start(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.lockConn != nil {
		return errors.New("task service already started")
	}
	schema, err := queueSchema(s.store.Pool())
	if err != nil {
		return err
	}
	conn, err := s.store.Pool().Acquire(ctx)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("shuttle:instance:%s", schema)
	var locked bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", key).Scan(&locked)
	if err != nil || !locked {
		conn.Release()
		return errors.New("another task instance is active or instance lock unavailable")
	}
	s.lockConn = conn
	s.lockKey = key
	if _, err = s.store.RecoverUnknownDeliveries(ctx); err != nil {
		s.releaseLock(ctx)
		return err
	}
	if err = s.queue.Start(ctx); err != nil {
		s.releaseLock(ctx)
		return err
	}
	watchCtx, watchCancel := context.WithCancel(ctx)
	s.watchCancel = watchCancel
	s.watchDone = make(chan struct{})
	go s.watchLock(watchCtx, conn, s.watchDone)
	return nil
}
func (s *Service) releaseLock(parent context.Context) {
	if s.lockConn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	defer cancel()
	_, err := s.lockConn.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended($1,0))", s.lockKey)
	if err != nil {
		_ = s.lockConn.Conn().Close(ctx)
	}
	s.lockConn.Release()
	s.lockConn = nil
}
func (s *Service) watchLock(ctx context.Context, conn *pgxpool.Conn, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, time.Second)
			err := conn.Conn().Ping(checkCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				select {
				case s.failures <- errors.New("task instance lock lost"):
				default:
				}
				s.logger.ErrorContext(ctx, "instance lock connection lost", "class", "instance_lock_lost")
				stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), lockLossStopTimeout)
				_ = s.queue.StopAndCancel(stopCtx)
				stopCancel()
				return
			}
		}
	}
}

// Failures reports a fatal service failure so the application stops admission.
func (s *Service) Failures() <-chan error { return s.failures }
func (s *Service) Stop(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.watchCancel != nil {
		s.watchCancel()
		select {
		case <-s.watchDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := s.queue.StopAndCancel(ctx); err != nil {
		return err
	}
	s.releaseLock(ctx)
	return nil
}
func (s *Service) accept(ctx context.Context, update telegram.Update, raw json.RawMessage) (db.Task, bool, error) {
	request, err := normalize(update, raw)
	if err != nil {
		return db.Task{}, false, err
	}
	if s.observer != nil {
		carrier := propagation.MapCarrier{}
		s.observer.Propagator().Inject(ctx, carrier)
		request.TraceParent = carrier.Get("traceparent")
		request.TraceState = carrier.Get("tracestate")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return db.Task{}, false, err
	}
	profile, _ := json.Marshal(request.User)
	return s.store.Intake(
		ctx,
		db.IntakeRequest{
			BotID:          s.options.BotID,
			UpdateID:       update.ID,
			TelegramUserID: request.User.ID,
			ChatID:         request.ChatID,
			Profile:        profile,
			Payload:        payload,
		},
		func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
			_, insertErr := s.queue.InsertTx(ctx, tx, JobArgs{TaskID: id}, nil)
			return insertErr
		},
	)
}
func (s *Service) record(ctx context.Context, component, outcome string) {
	if s.observer != nil {
		s.observer.IncContext(ctx, component, outcome)
	}
}
func (s *Service) Accept(ctx context.Context, update telegram.Update, raw json.RawMessage) (db.Task, bool, error) {
	task, duplicate, err := s.accept(ctx, update, raw)
	outcome := "accepted"
	switch {
	case err != nil:
		outcome = "rejected"
	case duplicate:
		outcome = "duplicate"
	}
	s.record(ctx, "webhook", outcome)
	return task, duplicate, err
}
