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

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/miniapp"
	"github.com/complynx/zns-chatbot/internal/observability"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	queueCleanupMargin         = 2 * time.Second
	minimumTaskPoolConnections = 2
	metricSuccess              = "success"
	metricError                = "error"
	metricRetry                = "retry"
)

type Options struct {
	AvatarRenderer                              avatar.Processor
	RegistrationDetailsEnabled                  bool
	MiniAppURL                                  string
	BotID                                       int64
	PollInterval, AttemptTimeout, LeaseDuration time.Duration
	MaxWorkers                                  int
}
type Service struct {
	keyboardSend                  [keyboardLockCount]sync.Mutex
	avatarDeliverySend            [avatarDeliveryLockCount]sync.Mutex
	store                         *db.Store
	iam                           *identity.Client
	loom                          *loom.Client
	tg                            *telegram.Client
	observer                      *observability.Telemetry
	logger                        *slog.Logger
	options                       Options
	queue                         *river.Client[pgx.Tx]
	owner                         string
	startMu                       sync.Mutex
	lockConn                      *pgxpool.Conn
	lockKey                       string
	watchCancel                   context.CancelFunc
	watchDone                     chan struct{}
	typingDone                    chan struct{}
	reactionDone                  chan struct{}
	menuDone                      chan struct{}
	menuMu                        sync.Mutex
	menuApplied                   map[int64]string
	failures                      chan error
	diagnosticMu                  sync.Mutex
	nextDatabaseDiagnostic        time.Time
	notificationCursor            uuid.UUID
	pairExpiryCursor              uuid.UUID
	knowledgeArchiveCursor        uuid.UUID
	avatarSourceExpiryCursor      uuid.UUID
	knowledgeNotificationCursor   uuid.UUID
	pairNotificationCursor        uuid.UUID
	avatarDeliveryCursor          uuid.UUID
	orderNotificationCursor       uuid.UUID
	massageReminderCursor         uuid.UUID
	massageSpecialistNoticeCursor uuid.UUID
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
	started := time.Now()
	err := w.service.work(ctx, job.Args.TaskID)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	outcome := metricSuccess
	if err != nil {
		outcome = metricRetry
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
	// The instance lease holds one pooled connection throughout execution.
	// Startup and workers must still be able to acquire another connection.
	if store.Pool() == nil || store.Pool().Config().MaxConns < minimumTaskPoolConnections {
		return nil, errors.New("task database pool requires at least two connections")
	}
	if o.MiniAppURL != "" && !miniapp.ValidLaunchURL(o.MiniAppURL) {
		return nil, errors.New("mini app launch URL must use canonical HTTPS and end in /app/")
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
	river.AddWorker(workers, &profileNameSyncWorker{service: s})
	river.AddWorker(workers, &notificationWorker{service: s})
	river.AddWorker(workers, &pairNotificationWorker{service: s})
	river.AddWorker(workers, &orderNotificationWorker{service: s})
	river.AddWorker(workers, &knowledgeNotificationWorker{service: s})
	river.AddWorker(workers, &avatarDeliveryWorker{service: s})
	river.AddWorker(workers, &massageReminderWorker{service: s})
	river.AddWorker(workers, &massageSpecialistNoticeWorker{service: s})
	schema, err := queueSchema(store.Pool())
	if err != nil {
		return nil, err
	}
	jobTimeout := o.AttemptTimeout + queueCleanupMargin
	// A crashed attempt must not wait River's one-hour default. Live attempts
	// still keep their full timeout and lease before native rescue can retry.
	rescueAfter := max(o.LeaseDuration, jobTimeout) + time.Second
	s.queue, err = river.NewClient(
		riverpgxv5.New(store.Pool()),
		&river.Config{
			Schema:               schema,
			Workers:              workers,
			Queues:               map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: o.MaxWorkers}},
			JobTimeout:           jobTimeout,
			RescueStuckJobsAfter: rescueAfter,
			Logger:               logger,
		},
	)
	return s, err
}

// Recovery runs under the exclusive service lock and before queue workers start.
func (s *Service) recoverBusinessNotifications(ctx context.Context) error {
	scope := s.notificationScope()
	for _, recoverNotices := range []func(context.Context, db.EventScope) error{
		s.store.RecoverNotifications, s.store.RecoverPairNotifications, s.store.RecoverOrderNotifications,
		s.store.RecoverAvatarDeliveries,
		s.store.RecoverMassageReminders, s.store.RecoverMassageSpecialistNotifications, s.store.RecoverKnowledgeNotifications,
	} {
		if err := recoverNotices(ctx, scope); err != nil {
			return err
		}
	}
	return nil
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
	botID, organization, issuer := s.iam.DeploymentScope()
	if botID != s.options.BotID {
		s.releaseLock(ctx)
		return db.ErrDeploymentScope
	}
	if err = s.store.BindDeployment(ctx, botID, organization, issuer); err != nil {
		s.releaseLock(ctx)
		return err
	}
	if _, err = s.store.RecoverUnknownDeliveries(ctx); err != nil {
		s.releaseLock(ctx)
		return err
	}
	if err = s.store.RecoverUnknownReactions(ctx); err != nil {
		s.releaseLock(ctx)
		return err
	}
	if err = s.recoverBusinessNotifications(ctx); err != nil {
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
	s.typingDone = make(chan struct{})
	s.reactionDone = make(chan struct{})
	s.menuDone = make(chan struct{})
	go s.watchLock(watchCtx, conn, s.watchDone)
	go s.watchTyping(watchCtx, s.typingDone)
	go s.watchReactions(watchCtx, s.reactionDone)
	go s.watchMenus(watchCtx, s.menuDone)
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
	defer s.watchCancel()
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
			noticeCtx, noticeCancel := context.WithTimeout(ctx, time.Second)
			err = s.scheduleNotifications(noticeCtx)
			noticeCancel()
			if err != nil && ctx.Err() == nil {
				s.record(ctx, "notifications", "error")
				s.logger.WarnContext(ctx, "notification scheduling unavailable", "class", "notification_queue")
			}
			s.repairPairNotifications(ctx)
			s.repairProfileNames(ctx)
			s.repairOrderNotifications(ctx)
			s.repairKnowledgeNotifications(ctx)
			s.repairAvatarDeliveries(ctx)
			s.repairMassageReminders(ctx)
			s.repairMassageSpecialistNotices(ctx)
			s.archiveEndedKnowledge(ctx)
			s.expireAvatarSources(ctx)
			expiryCtx, expiryCancel := context.WithTimeout(ctx, time.Second)
			next, expiryErr := s.store.ExpirePairInvitations(expiryCtx, s.notificationScope(), s.pairExpiryCursor)
			expiryCancel()
			if expiryErr == nil {
				s.pairExpiryCursor = next
			} else if ctx.Err() == nil {
				s.record(ctx, "pair_expiry", metricError)
				s.logger.WarnContext(ctx, "pair invitation expiry unavailable", "class", "pair_expiry")
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
		select {
		case <-s.typingDone:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-s.reactionDone:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-s.menuDone:
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
	if err = s.routeStart(ctx, &request); err != nil {
		return db.Task{}, false, err
	}
	if s.observer != nil {
		carrier := propagation.MapCarrier{}
		s.observer.Propagator().Inject(ctx, carrier)
		request.TraceParent = carrier.Get("traceparent")
		request.TraceState = carrier.Get("tracestate")
	}
	request.PreferredLanguage = s.languageFor(ctx, request)
	payload, err := json.Marshal(request)
	if err != nil {
		return db.Task{}, false, err
	}
	profile, _ := json.Marshal(request.User)
	return s.store.Intake(
		ctx,
		db.IntakeRequest{
			Symbols:        request.Symbols,
			Attachments:    request.Attachments,
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
	if update.CallbackQuery != nil && update.CallbackQuery.ID != "" &&
		(err == nil || errors.Is(err, ErrUnsupportedUpdate)) {
		// Callback acknowledgement affects only the Telegram spinner, not durable acceptance.
		ack, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		ackErr := s.tg.AnswerCallbackQuery(ack, update.CallbackQuery.ID, "")
		cancel()
		if ackErr != nil {
			s.logger.WarnContext(ctx, "callback acknowledgement unavailable")
		}
	}
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
