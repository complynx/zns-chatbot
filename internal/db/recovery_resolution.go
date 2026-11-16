package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// RecoveryReconciliationPrefix reserves audit keys separately from local closure.
const RecoveryReconciliationPrefix = "recovery-reconcile:"

const RecoveryAbandonmentPrefix = "recovery-abandon:"

// IsRecoveryOperationID keeps external receipt audits separate from local closure keys.
func IsRecoveryOperationID(id string) bool {
	return strings.HasPrefix(id, RecoveryReconciliationPrefix) || strings.HasPrefix(id, RecoveryAbandonmentPrefix)
}

// SaveRecoveryWithdrawal commits the authoritative receipt, local fence and
// operator audit together. An unknown upstream result must never call this.
func (s *Store) SaveRecoveryWithdrawal(ctx context.Context, t Task, owner string,
	in CloseTaskRequest) (TaskResolution, error) {
	if t.ID != in.TaskID || !strings.HasPrefix(in.OperationID, RecoveryAbandonmentPrefix) {
		return TaskResolution{}, ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaskResolution{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	locked, err := q.LockAdminTask(ctx, sqlc.LockAdminTaskParams{ID: t.ID, BotID: in.BotID, Org: in.Organization})
	if err != nil {
		return TaskResolution{}, err
	}
	if !recoveryWithdrawalProof(locked.Snapshot, t.Snapshot) {
		return TaskResolution{}, ErrConflict
	}
	updated, err := tx.Exec(ctx, `UPDATE shuttle_tasks SET snapshot=$4,lease_owner='',lease_until=NULL
	 WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND version=$5 AND lease_until>now()`,
		t.ID, owner, t.Fence, t.Snapshot, t.Version)
	if err != nil {
		return TaskResolution{}, err
	}
	if updated.RowsAffected() != 1 {
		return TaskResolution{}, ErrLeaseLost
	}
	result, err := s.closeTaskTx(ctx, q, locked, in, t.Version)
	if err != nil {
		return TaskResolution{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TaskResolution{}, err
	}
	return result, nil
}

func recoveryWithdrawalProof(before, after []byte) bool {
	var old, saved recoveryCheckpoint
	if json.Unmarshal(before, &old) != nil || json.Unmarshal(after, &saved) != nil ||
		old.Recovery == nil || old.Recovery.Receipt == nil || saved.Recovery == nil || saved.Recovery.Receipt == nil {
		return false
	}
	prior, receipt := old.Recovery.Receipt, saved.Recovery.Receipt
	return prior.MessageID != "" && !prior.Delivered && !prior.Abandoned && receipt.MessageID == prior.MessageID &&
		receipt.Abandoned && !receipt.Delivered
}

// SaveRecoveryReconciliation commits the receipt, audit and observation job
// together. The caller preserves unknown renewal evidence in the snapshot.
func (s *Store) SaveRecoveryReconciliation(ctx context.Context, t Task, owner string,
	in CloseTaskRequest, enqueue func(context.Context, pgx.Tx, uuid.UUID) error) (TaskResolution, error) {
	if t.ID != in.TaskID || !strings.HasPrefix(in.OperationID, RecoveryReconciliationPrefix) {
		return TaskResolution{}, ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TaskResolution{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int64
	var previous string
	err = tx.QueryRow(ctx, `SELECT state FROM shuttle_tasks WHERE id=$1 AND version=$2
	 AND state IN ('auth_unknown','delivery_unknown') FOR UPDATE`, t.ID, in.ExpectedVersion).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskResolution{}, ErrConflict
	}
	if err != nil {
		return TaskResolution{}, err
	}
	err = tx.QueryRow(ctx, `UPDATE shuttle_tasks SET snapshot=$4,state=$6,version=version+1
	 WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>now()
	 AND version=$5 RETURNING version`,
		t.ID, owner, t.Fence, t.Snapshot, in.ExpectedVersion, t.State).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskResolution{}, ErrConflict
	}
	if err != nil {
		return TaskResolution{}, err
	}
	saved, err := s.q.WithTx(tx).InsertTaskResolution(ctx, sqlc.InsertTaskResolutionParams{
		TaskID: t.ID, OperationID: in.OperationID, Actor: in.Actor, Reason: in.Reason,
		ExpectedVersion: in.ExpectedVersion, ResultingVersion: version, PreviousState: previous,
	})
	if err != nil {
		return TaskResolution{}, err
	}
	if t.State == "awaiting_service" {
		if err = enqueue(ctx, tx, t.ID); err != nil {
			return TaskResolution{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return TaskResolution{}, err
	}
	return resolution(saved), nil
}
