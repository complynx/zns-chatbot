package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const taskClosurePreparationTTL = 5 * time.Minute

var (
	ErrTaskClosureInvalid = errors.New("invalid task closure")
)

type TaskClosureArguments struct {
	TaskID          uuid.UUID `json:"task_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
	Reason          string    `json:"reason"                       minLength:"1" maxLength:"512"`
}

type TaskClosurePreparation struct {
	ID          uuid.UUID            `json:"proposal_id"`
	Arguments   TaskClosureArguments `json:"arguments"`
	Description string               `json:"description"`
	ExpiresAt   time.Time            `json:"expires_at"`
}

type TaskClosureResult struct {
	TaskID     uuid.UUID      `json:"task_id"`
	Resolution TaskResolution `json:"resolution"`
}

func normalizedTaskClosure(args TaskClosureArguments) (TaskClosureArguments, []byte, error) {
	args.Reason = strings.TrimSpace(args.Reason)
	if args.TaskID == uuid.Nil || args.ExpectedVersion < 1 || len(args.Reason) == 0 ||
		utf8.RuneCountInString(args.Reason) > 512 {
		return args, nil, ErrTaskClosureInvalid
	}
	raw, err := json.Marshal(args)
	hash := sha256.Sum256(raw)
	return args, hash[:], err
}

func taskClosureKey(scope AdminScope, op string) sqlc.GetTaskClosureProposalParams {
	return sqlc.GetTaskClosureProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
	}
}

func (s *Store) taskClosureAvailable(
	ctx context.Context, q *sqlc.Queries, task sqlc.ShuttleTask, args TaskClosureArguments, op string,
) (time.Time, error) {
	if task.Version != args.ExpectedVersion {
		return time.Time{}, ErrConflict
	}
	if err := s.requireUncertainTask(ctx, q, task); err != nil {
		return time.Time{}, err
	}
	_, err := q.GetTaskResolution(ctx, sqlc.GetTaskResolutionParams{TaskID: task.ID, OperationID: op})
	if err == nil {
		return time.Time{}, ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if task.LeaseUntil.Valid && task.LeaseUntil.Time.After(now.Time) {
		return time.Time{}, ErrConflict
	}
	return now.Time, nil
}

// PrepareTaskClosure stores exact local effects without closing or leasing the task.
func (s *Store) PrepareTaskClosure(
	ctx context.Context, scope AdminScope, op string, args TaskClosureArguments,
) (TaskClosurePreparation, error) {
	args, hash, err := normalizedTaskClosure(args)
	if err != nil {
		return TaskClosurePreparation{}, err
	}
	tx, q, err := s.beginAdminOperation(ctx, scope, "task-closure", op)
	if err != nil {
		return TaskClosurePreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetTaskClosureProposal(ctx, taskClosureKey(scope, op))
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return TaskClosurePreparation{}, ErrConflict
		}
		if _, err = q.GetAdminTask(
			ctx,
			sqlc.GetAdminTaskParams{ID: prior.TaskID, BotID: scope.BotID, Org: scope.Organization},
		); err != nil {
			return TaskClosurePreparation{}, err
		}
		var result TaskClosurePreparation
		err = json.Unmarshal(prior.Document, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TaskClosurePreparation{}, err
	}
	task, err := q.LockAdminTask(
		ctx,
		sqlc.LockAdminTaskParams{ID: args.TaskID, BotID: scope.BotID, Org: scope.Organization},
	)
	if err != nil {
		return TaskClosurePreparation{}, err
	}
	now, err := s.taskClosureAvailable(ctx, q, task, args, op)
	if err != nil {
		return TaskClosurePreparation{}, err
	}
	result := TaskClosurePreparation{
		ID:        uuid.New(),
		Arguments: args,
		ExpiresAt: now.Add(taskClosurePreparationTTL),
		Description: fmt.Sprintf(
			"Close local task %s for account %s, observed state %s, version %d. Reaso"+
				"n: %s. Stop local processing and block pending/retryable delivery; prese"+
				"rve uncertain evidence. This does not establish the external outcome, re"+
				"try delivery, cancel a remote Loom run or change money.",
			task.ID,
			task.UserID,
			task.State,
			task.Version,
			args.Reason,
		),
	}
	document, err := json.Marshal(result)
	if err != nil {
		return TaskClosurePreparation{}, err
	}
	err = q.InsertTaskClosureProposal(ctx, sqlc.InsertTaskClosureProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
		ProposalID: result.ID, TaskID: task.ID, RequestHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true}, Document: document,
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

// ConfirmTaskClosure commits closure and its agent receipt together. Only a result
// bound to this proposal can replay; an operator receipt is never adopted.
func (s *Store) ConfirmTaskClosure(
	ctx context.Context, scope AdminScope, op string, args TaskClosureArguments, proposal uuid.UUID,
) (TaskClosureResult, error) {
	args, hash, err := normalizedTaskClosure(args)
	if err != nil {
		return TaskClosureResult{}, err
	}
	if proposal == uuid.Nil {
		return TaskClosureResult{}, ErrTaskClosureInvalid
	}
	tx, q, err := s.beginAdminOperation(ctx, scope, "task-closure", op)
	if err != nil {
		return TaskClosureResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetTaskClosureProposal(ctx, taskClosureKey(scope, op))
	if err != nil {
		return TaskClosureResult{}, err
	}
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return TaskClosureResult{}, ErrConflict
	}
	if len(prior.Result) != 0 {
		return readTaskClosureResult(ctx, q, scope, prior)
	}
	task, err := q.LockAdminTask(
		ctx,
		sqlc.LockAdminTaskParams{ID: args.TaskID, BotID: scope.BotID, Org: scope.Organization},
	)
	if err != nil {
		return TaskClosureResult{}, err
	}
	now, err := s.taskClosureAvailable(ctx, q, task, args, op)
	if err != nil {
		return TaskClosureResult{}, err
	}
	if !now.Before(prior.ExpiresAt.Time) {
		return TaskClosureResult{}, ErrConflict
	}
	resolution, err := s.closeUncertainTaskTx(ctx, q, task, CloseTaskRequest{
		TaskID: task.ID, BotID: scope.BotID, Organization: scope.Organization, Actor: scope.Subject,
		OperationID: op, Reason: args.Reason, ExpectedVersion: args.ExpectedVersion,
	})
	if err != nil {
		return TaskClosureResult{}, err
	}
	result := TaskClosureResult{TaskID: task.ID, Resolution: resolution}
	receipt, err := json.Marshal(result)
	if err != nil {
		return TaskClosureResult{}, err
	}
	saved, err := q.SaveTaskClosureResult(ctx, sqlc.SaveTaskClosureResultParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op, Result: receipt,
	})
	if err == nil && saved != 1 {
		err = ErrConflict
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

func readTaskClosureResult(
	ctx context.Context, q *sqlc.Queries, scope AdminScope, row sqlc.ShuttleTaskClosureProposal,
) (TaskClosureResult, error) {
	if _, err := q.GetAdminTask(
		ctx,
		sqlc.GetAdminTaskParams{ID: row.TaskID, BotID: scope.BotID, Org: scope.Organization},
	); err != nil {
		return TaskClosureResult{}, err
	}
	if len(row.Result) == 0 {
		return TaskClosureResult{}, pgx.ErrNoRows
	}
	var result TaskClosureResult
	err := json.Unmarshal(row.Result, &result)
	return result, err
}

// TaskClosureReceipt returns only this active actor's prepared agent operation.
func (s *Store) TaskClosureReceipt(ctx context.Context, scope AdminScope, op string) (*TaskClosureResult, error) {
	tx, q, err := s.beginAdminOperation(ctx, scope, "task-closure", op)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := q.GetTaskClosureProposal(ctx, taskClosureKey(scope, op))
	if err != nil {
		return nil, err
	}
	result, err := readTaskClosureResult(ctx, q, scope, row)
	return &result, err
}
