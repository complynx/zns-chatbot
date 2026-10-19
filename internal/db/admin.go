package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

type TaskSummary struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"user_id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	State          string     `json:"state"`
	Route          string     `json:"route"`
	RunID          string     `json:"run_id,omitempty"`
	Version        int64      `json:"version"`
	Fence          int64      `json:"fence"`
	CreatedAt      time.Time  `json:"created_at"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	HasAuthSession bool       `json:"has_auth_session"`
}

type DeliverySummary struct {
	ID        uuid.UUID `json:"id"`
	ChunkSeq  int32     `json:"chunk_seq"`
	State     string    `json:"state"`
	AttemptID uuid.UUID `json:"attempt_id"`
}

type QueueJobSummary struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"`
	Attempt     int16     `json:"attempt"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

type TaskResolution struct {
	OperationID      string    `json:"operation_id"`
	Actor            string    `json:"actor"`
	ExpectedVersion  int64     `json:"expected_version"`
	ResultingVersion int64     `json:"resulting_version"`
	Reason           string    `json:"reason"`
	PreviousState    string    `json:"previous_state"`
	CreatedAt        time.Time `json:"created_at"`
}

type TaskDiagnostics struct {
	Task        TaskSummary       `json:"task"`
	Deliveries  []DeliverySummary `json:"deliveries"`
	QueueJobs   []QueueJobSummary `json:"queue_jobs"`
	Resolutions []TaskResolution  `json:"resolutions"`
}

type CloseTaskRequest struct {
	TaskID                                   uuid.UUID
	BotID                                    int64
	Organization, OperationID, Actor, Reason string
	ExpectedVersion                          int64
}

func taskSummary(t sqlc.ShuttleTask) TaskSummary {
	result := TaskSummary{
		ID: t.ID, UserID: t.UserID, ConversationID: t.ConversationID, State: t.State, Route: t.Route,
		RunID: t.RunID, Version: t.Version, Fence: t.Fence, CreatedAt: t.CreatedAt.Time,
		HasAuthSession: t.AuthSessionID != "",
	}
	if t.LeaseUntil.Valid {
		result.LeaseUntil = &t.LeaseUntil.Time
	}
	return result
}

func resolution(r sqlc.ShuttleTaskResolution) TaskResolution {
	return TaskResolution{
		r.OperationID,
		r.Actor,
		r.ExpectedVersion,
		r.ResultingVersion,
		r.Reason,
		r.PreviousState,
		r.CreatedAt.Time,
	}
}

// ListAdminTasks uses a stable UUID cursor and never returns message content or credentials.
func (s *Store) ListAdminTasks(
	ctx context.Context,
	botID int64,
	org, state string,
	after uuid.UUID,
	limit int32,
) ([]TaskSummary, error) {
	rows, err := s.q.ListAdminTasks(
		ctx,
		sqlc.ListAdminTasksParams{BotID: botID, Org: org, State: state, AfterID: after, PageLimit: limit},
	)
	if err != nil {
		return nil, err
	}
	result := make([]TaskSummary, 0, len(rows))
	for _, r := range rows {
		result = append(result, taskSummary(r))
	}
	return result, nil
}

func (s *Store) TaskDiagnostics(ctx context.Context, id uuid.UUID, botID int64, org string) (TaskDiagnostics, error) {
	t, err := s.q.GetAdminTask(ctx, sqlc.GetAdminTaskParams{ID: id, BotID: botID, Org: org})
	if err != nil {
		return TaskDiagnostics{}, err
	}
	result := TaskDiagnostics{
		Task:        taskSummary(t),
		Deliveries:  []DeliverySummary{},
		QueueJobs:   []QueueJobSummary{},
		Resolutions: []TaskResolution{},
	}
	out, err := s.q.ListOutbox(ctx, id)
	if err != nil {
		return result, err
	}
	for _, row := range out {
		result.Deliveries = append(
			result.Deliveries,
			DeliverySummary{row.ID, row.ChunkSeq, row.State, uuid.UUID(row.AttemptID.Bytes)},
		)
	}
	audit, err := s.q.ListTaskResolutions(ctx, id)
	if err != nil {
		return result, err
	}
	for _, row := range audit {
		result.Resolutions = append(result.Resolutions, resolution(row))
	}
	result.QueueJobs, err = s.taskQueueJobs(ctx, id)
	return result, err
}

// River owns this schema. Read only its pinned public job metadata; never expose errors or args.
func (s *Store) taskQueueJobs(ctx context.Context, id uuid.UUID) ([]QueueJobSummary, error) {
	rows, err := s.pool.Query(
		ctx,
		`SELECT id,state::text,attempt,scheduled_at FROM river_job WHERE kind='shuttle_task' AND args->>'task_id'=$1 ORDER BY id`,
		id.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []QueueJobSummary{}
	for rows.Next() {
		var job QueueJobSummary
		if err = rows.Scan(&job.ID, &job.State, &job.Attempt, &job.ScheduledAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// CloseUncertainTask ends local processing without claiming or replaying an external result.
func (s *Store) CloseUncertainTask(ctx context.Context, in CloseTaskRequest) (TaskResolution, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaskResolution{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	t, err := q.LockAdminTask(ctx, sqlc.LockAdminTaskParams{ID: in.TaskID, BotID: in.BotID, Org: in.Organization})
	if err != nil {
		return TaskResolution{}, err
	}
	previous, err := q.GetTaskResolution(
		ctx,
		sqlc.GetTaskResolutionParams{TaskID: in.TaskID, OperationID: in.OperationID},
	)
	if err == nil {
		if previous.Actor != in.Actor || previous.Reason != in.Reason ||
			previous.ExpectedVersion != in.ExpectedVersion {
			return TaskResolution{}, ErrConflict
		}
		return resolution(previous), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TaskResolution{}, err
	}
	result, err := s.closeUncertainTaskTx(ctx, q, t, in)
	if err != nil {
		return TaskResolution{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TaskResolution{}, err
	}
	return result, nil
}

// closeUncertainTaskTx applies local closure to a locked task. Its caller owns
// receipt replay and the transaction, so an agent proposal can commit atomically.
func (s *Store) closeUncertainTaskTx(
	ctx context.Context,
	q *sqlc.Queries,
	t sqlc.ShuttleTask,
	in CloseTaskRequest,
) (TaskResolution, error) {
	if err := s.requireUncertainTask(ctx, q, t); err != nil {
		return TaskResolution{}, err
	}
	closed, err := q.CloseUncertainTask(ctx, sqlc.CloseUncertainTaskParams{ID: t.ID, Version: in.ExpectedVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskResolution{}, ErrConflict
	}
	if err != nil {
		return TaskResolution{}, err
	}
	if err = q.BlockDeliverySuffix(ctx, sqlc.BlockDeliverySuffixParams{TaskID: t.ID, ChunkSeq: -1}); err != nil {
		return TaskResolution{}, err
	}
	if err = q.ClearConversationRun(
		ctx,
		sqlc.ClearConversationRunParams{ID: t.ConversationID, ActiveTaskID: pgtype.UUID{Bytes: t.ID, Valid: true}},
	); err != nil {
		return TaskResolution{}, err
	}
	saved, err := q.InsertTaskResolution(
		ctx,
		sqlc.InsertTaskResolutionParams{
			TaskID:           t.ID,
			OperationID:      in.OperationID,
			Actor:            in.Actor,
			ExpectedVersion:  in.ExpectedVersion,
			ResultingVersion: closed.Version,
			Reason:           in.Reason,
			PreviousState:    t.State,
		},
	)
	if err != nil {
		return TaskResolution{}, err
	}
	return resolution(saved), nil
}

func (s *Store) requireUncertainTask(ctx context.Context, q *sqlc.Queries, t sqlc.ShuttleTask) error {
	if t.State == "closed" {
		return ErrConflict
	}
	if t.State == "auth_unknown" {
		return nil
	}
	var checkpoint struct {
		Reauthorizing bool `json:"reauthorizing"`
	}
	if json.Unmarshal(t.Snapshot, &checkpoint) == nil && checkpoint.Reauthorizing {
		return nil
	}
	rows, err := q.ListOutbox(ctx, t.ID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.State == deliveryUnknown || row.State == "send_inflight" {
			return nil
		}
	}
	return ErrConflict
}
