package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/complynx/shuttle/internal/db/sqlc"
	"github.com/complynx/shuttle/migrations"
)

type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{p, sqlc.New(p)}, nil
}
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
func (s *Store) Close()              { s.pool.Close() }
func (s *Store) Migrate(ctx context.Context) error {
	d := stdlib.OpenDBFromPool(s.pool)
	defer d.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, d, migrations.Files)
	if err != nil {
		return err
	}
	_, err = p.Up(ctx)
	return err
}
func task(t sqlc.ShuttleTask) Task {
	return Task{
		ID:             t.ID,
		UserID:         t.UserID,
		ConversationID: t.ConversationID,
		TargetTaskID:   t.TargetTaskID,
		Seq:            t.Seq,
		Request:        t.Request,
		State:          t.State,
		Route:          t.Route,
		TargetRunID:    t.TargetRunID,
		RunID:          t.RunID,
		AuthSessionID:  t.AuthSessionID,
		QuestionID:     t.QuestionID,
		Snapshot:       t.Snapshot,
		Version:        t.Version,
		Fence:          t.Fence,
		LeaseOwner:     t.LeaseOwner,
	}
}
func user(u sqlc.ShuttleUser) User {
	return User{u.ID, u.BotID, u.TgUserID, u.Profile, u.ZitadelSubject, u.ZitadelOrg, u.Revoked}
}
func (s *Store) GetTask(ctx context.Context, id uuid.UUID) (Task, error) {
	t, e := s.q.GetTask(ctx, id)
	return task(t), e
}
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, e := s.q.GetUser(ctx, id)
	return user(u), e
}
func (s *Store) GetUserBySubject(ctx context.Context, subject string) (User, error) {
	u, e := s.q.GetUserBySubject(ctx, subject)
	return user(u), e
}
func (s *Store) BindSubject(ctx context.Context, id uuid.UUID, subject, org string) (User, error) {
	if subject == "" {
		return User{}, fmt.Errorf("empty subject")
	}
	u, e := s.q.BindIdentity(ctx, sqlc.BindIdentityParams{ID: id, ZitadelSubject: subject, ZitadelOrg: org})
	return user(u), e
}
func (s *Store) RevokeUser(ctx context.Context, id uuid.UUID, revoked bool) error {
	_, e := s.q.RevokeUser(ctx, sqlc.RevokeUserParams{ID: id, Revoked: revoked})
	return e
}

// Intake commits the immutable accepted request and its queue job together.
func (s *Store) Intake(
	ctx context.Context,
	r IntakeRequest,
	enqueue func(context.Context, pgx.Tx, uuid.UUID) error,
) (Task, bool, error) {
	if !json.Valid(r.Payload) {
		return Task{}, false, fmt.Errorf("invalid intake payload")
	}
	if len(r.Profile) == 0 {
		r.Profile = json.RawMessage(`{}`)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Task{}, false, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// One update key serializes duplicates without holding a conversation for a run.
	_, e = tx.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1,0))",
		fmt.Sprintf("shuttle:%d:%d", r.BotID, r.UpdateID),
	)
	if e != nil {
		return Task{}, false, e
	}
	q := s.q.WithTx(tx)
	id, e := q.FindInbox(ctx, sqlc.FindInboxParams{BotID: r.BotID, UpdateID: r.UpdateID})
	if e == nil {
		existing, lookupErr := q.GetTask(ctx, id)
		return task(existing), true, lookupErr
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return Task{}, false, e
	}
	u, e := q.UpsertUser(
		ctx,
		sqlc.UpsertUserParams{ID: uuid.New(), BotID: r.BotID, TgUserID: r.TelegramUserID, Profile: r.Profile},
	)
	if e != nil {
		return Task{}, false, e
	}
	c, e := q.EnsureConversation(ctx, sqlc.EnsureConversationParams{ID: uuid.New(), UserID: u.ID, ChatID: r.ChatID})
	if e != nil {
		return Task{}, false, e
	}
	seq, e := q.NextSequence(ctx, c.ID)
	if e != nil {
		return Task{}, false, e
	}
	t, e := q.InsertTask(
		ctx,
		sqlc.InsertTaskParams{ID: uuid.New(), UserID: u.ID, ConversationID: c.ID, Seq: seq, Request: r.Payload},
	)
	if e != nil {
		return Task{}, false, e
	}
	if e = q.InsertInbox(
		ctx,
		sqlc.InsertInboxParams{BotID: r.BotID, UpdateID: r.UpdateID, TaskID: t.ID, Payload: r.Payload},
	); e != nil {
		return Task{}, false, e
	}
	if enqueue != nil {
		if e = enqueue(ctx, tx, t.ID); e != nil {
			return Task{}, false, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Task{}, false, e
	}
	return task(t), false, nil
}

// Route only holds the short inbox ordering gate. Active Loom runs hold no lock.
func (s *Store) Route(ctx context.Context, id uuid.UUID) (Task, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Task{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	t, e := q.GetTask(ctx, id)
	if e != nil {
		return Task{}, e
	}
	c, e := q.LockConversation(ctx, t.ConversationID)
	if e != nil {
		return Task{}, e
	}
	t, e = q.GetTask(ctx, id)
	if e != nil {
		return Task{}, e
	}
	if t.Route != "" {
		return task(t), nil
	}
	if t.Seq != c.RoutedSeq+1 {
		return Task{}, ErrRoutingPending
	}
	var envelope struct {
		Kind       string `json:"kind"`
		QuestionID string `json:"question_id"`
	}
	if e = json.Unmarshal(t.Request, &envelope); e != nil {
		return Task{}, e
	}
	route := "new_run"
	target := uuid.Nil
	run, question := "", envelope.QuestionID
	active := c.ActiveTaskID
	a, lookupErr := routingActiveTask(ctx, q, c, t.Seq)
	if lookupErr != nil {
		return Task{}, lookupErr
	}
	if a.ID != uuid.Nil {
		target = a.ID
		run = a.RunID
		question = a.QuestionID
		if envelope.QuestionID != "" {
			question = envelope.QuestionID
		}
		route = "input"
	} else {
		active = pgtype.UUID{}
	}
	if envelope.Kind == "cancel" || envelope.Kind == "approval" || envelope.Kind == "input" {
		route = envelope.Kind
		if target == uuid.Nil {
			route = "no_active"
		}
	}
	if route == "new_run" {
		active = pgtype.UUID{Bytes: t.ID, Valid: true}
	}
	t, e = q.SetRoute(
		ctx,
		sqlc.SetRouteParams{ID: id, Route: route, TargetRunID: run, QuestionID: question, TargetTaskID: target},
	)
	if e != nil {
		return Task{}, e
	}
	if e = q.AdvanceRouting(
		ctx,
		sqlc.AdvanceRoutingParams{ID: c.ID, RoutedSeq: t.Seq, ActiveTaskID: active},
	); e != nil {
		return Task{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Task{}, e
	}
	return task(t), nil
}
func (s *Store) Claim(ctx context.Context, id uuid.UUID, owner string, lease time.Duration) (Task, error) {
	if owner == "" || lease <= 0 {
		return Task{}, fmt.Errorf("invalid lease")
	}
	t, e := s.q.ClaimTask(ctx, sqlc.ClaimTaskParams{ID: id, LeaseOwner: owner, Column3: lease.Milliseconds()})
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrLeaseLost
	}
	return task(t), e
}
func (s *Store) Release(ctx context.Context, id uuid.UUID, owner string, fence int64) error {
	n, e := s.q.ReleaseTask(ctx, sqlc.ReleaseTaskParams{ID: id, LeaseOwner: owner, Fence: fence})
	if e == nil && n == 0 {
		return ErrLeaseLost
	}
	return e
}
func (s *Store) Save(ctx context.Context, t Task, owner string, fence int64, out []Outbound) (Task, error) {
	if len(t.Snapshot) == 0 {
		t.Snapshot = json.RawMessage(`{}`)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Task{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	saved, e := q.SaveTask(
		ctx,
		sqlc.SaveTaskParams{
			ID:            t.ID,
			LeaseOwner:    owner,
			Fence:         fence,
			State:         t.State,
			RunID:         t.RunID,
			AuthSessionID: t.AuthSessionID,
			QuestionID:    t.QuestionID,
			Snapshot:      t.Snapshot,
		},
	)
	if errors.Is(e, pgx.ErrNoRows) {
		return Task{}, ErrLeaseLost
	}
	if e != nil {
		return Task{}, e
	}
	if terminalTask(t.State) {
		e = q.ClearConversationRun(
			ctx,
			sqlc.ClearConversationRunParams{ID: t.ConversationID, ActiveTaskID: pgtype.UUID{Bytes: t.ID, Valid: true}},
		)
	} else {
		e = q.UpdateConversationRun(
			ctx,
			sqlc.UpdateConversationRunParams{
				ID:                 t.ConversationID,
				ActiveTaskID:       pgtype.UUID{Bytes: t.ID, Valid: true},
				ActiveRunID:        t.RunID,
				AwaitingQuestionID: t.QuestionID,
			},
		)
	}
	if e != nil {
		return Task{}, e
	}
	for _, o := range out {
		if o.ID == uuid.Nil {
			o.ID = uuid.New()
		}
		if e = q.InsertOutbox(
			ctx,
			sqlc.InsertOutboxParams{
				ID:       o.ID,
				TaskID:   t.ID,
				DedupKey: o.DedupKey,
				ChunkSeq: o.ChunkSeq,
				Payload:  o.Payload,
			},
		); e != nil {
			return Task{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Task{}, e
	}
	return task(saved), nil
}

func terminalTask(state string) bool {
	switch state {
	case "completed", "failed", "cancelled", "delivery_unknown", "delivery_failed":
		return true
	default:
		return false
	}
}
func activeTask(ctx context.Context, q *sqlc.Queries, id pgtype.UUID) (sqlc.ShuttleTask, error) {
	if !id.Valid {
		return sqlc.ShuttleTask{}, nil
	}
	t, err := q.GetTask(ctx, uuid.UUID(id.Bytes))
	if err != nil {
		return sqlc.ShuttleTask{}, err
	}
	if terminalTask(t.State) {
		return sqlc.ShuttleTask{}, nil
	}
	return t, nil
}

// ControlReady orders controls for one target run without holding its lifetime lock.
func (s *Store) ControlReady(ctx context.Context, taskID uuid.UUID) (bool, error) {
	return s.q.ControlReady(ctx, taskID)
}

// PromoteMessage preserves accepted text when its routed target has finished.
// Call only before any external input intent has been persisted or submitted.
func (s *Store) PromoteMessage(ctx context.Context, taskID uuid.UUID, owner string, fence int64) (Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	current, err := q.GetTask(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	conversation, err := q.LockConversation(ctx, current.ConversationID)
	if err != nil {
		return Task{}, err
	}
	target, err := q.GetTask(ctx, current.TargetTaskID)
	if err != nil {
		return Task{}, err
	}
	if !terminalTask(target.State) {
		return Task{}, ErrRoutingPending
	}
	active, err := activeTask(ctx, q, conversation.ActiveTaskID)
	if err != nil {
		return Task{}, err
	}
	if active.ID != uuid.Nil {
		return Task{}, ErrRoutingPending
	}
	promoted, err := q.PromoteTask(ctx, sqlc.PromoteTaskParams{ID: taskID, LeaseOwner: owner, Fence: fence})
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrLeaseLost
	}
	if err != nil {
		return Task{}, err
	}
	if err = q.ActivatePromotedTask(
		ctx,
		sqlc.ActivatePromotedTaskParams{
			ID:           current.ConversationID,
			ActiveTaskID: pgtype.UUID{Bytes: taskID, Valid: true},
		},
	); err != nil {
		return Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return task(promoted), nil
}

func routingActiveTask(
	ctx context.Context,
	q *sqlc.Queries,
	c sqlc.ShuttleConversation,
	seq int64,
) (sqlc.ShuttleTask, error) {
	active, err := activeTask(ctx, q, c.ActiveTaskID)
	if err != nil || active.ID != uuid.Nil {
		return active, err
	}
	pending, err := q.HasEarlierPromotableMessage(
		ctx,
		sqlc.HasEarlierPromotableMessageParams{ConversationID: c.ID, Seq: seq},
	)
	if err != nil {
		return sqlc.ShuttleTask{}, err
	}
	if pending {
		return sqlc.ShuttleTask{}, ErrRoutingPending
	}
	return active, nil
}
