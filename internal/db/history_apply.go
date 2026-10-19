package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

var (
	ErrInvalidHistory = errors.New("invalid historical import request")
	ErrHistoryQuota   = errors.New("historical import quota reached")
)

type HistoryApplyArguments struct {
	ArchiveID         uuid.UUID `json:"archive_id"`
	SHA256            string    `json:"sha256"`
	ProjectionVersion string    `json:"projection_version"`
}

type HistoryApplyPreparation struct {
	ID        uuid.UUID             `json:"proposal_id"`
	Arguments HistoryApplyArguments `json:"arguments"`
	ExpiresAt time.Time             `json:"expires_at"`
}

type HistoryApplyRequest struct {
	OperationID uuid.UUID
	ProposalID  uuid.UUID
	Arguments   HistoryApplyArguments
}

type HistoryApplyReceipt struct {
	ArchiveID         uuid.UUID `json:"archive_id"`
	SHA256            string    `json:"sha256"`
	ProjectionVersion string    `json:"projection_version"`
	AppliedAt         time.Time `json:"applied_at"`
	Records           int64     `json:"records"`
	Projected         int64     `json:"projected"`
	Inserted          int64     `json:"inserted"`
	Reused            int64     `json:"reused"`
}

func historyApplyHash(a HistoryApplyArguments) ([]byte, error) {
	digest, err := hex.DecodeString(a.SHA256)
	if a.ArchiveID == uuid.Nil || a.ProjectionVersion != legacyimport.ProjectionVersion ||
		err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != a.SHA256 {
		return nil, ErrInvalidHistory
	}
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return hash[:], err
}

// PrepareHistoryApply freezes the archive, digest and projection version. It does
// not apply records or infer authority from historical account fields.
func (s *Store) PrepareHistoryApply(ctx context.Context, scope ProfileScope, operation uuid.UUID,
	a HistoryApplyArguments) (HistoryApplyPreparation, error) {
	hash, err := historyApplyHash(a)
	if err != nil || operation == uuid.Nil {
		return HistoryApplyPreparation{}, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return HistoryApplyPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return HistoryApplyPreparation{}, err
	}
	if err = q.LockEventOperation(ctx, fmt.Sprintf("history-prepare:%s:%s", scope.UserID, operation)); err != nil {
		return HistoryApplyPreparation{}, err
	}
	prior, err := q.GetHistoryPreparation(
		ctx,
		sqlc.GetHistoryPreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return HistoryApplyPreparation{}, ErrConflict
		}
		return HistoryApplyPreparation{ID: prior.ID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return HistoryApplyPreparation{}, err
	}
	archive, err := q.GetLegacyArchive(
		ctx,
		sqlc.GetLegacyArchiveParams{ID: a.ArchiveID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return HistoryApplyPreparation{}, err
	}
	if archive.Sha256 != a.SHA256 {
		return HistoryApplyPreparation{}, ErrConflict
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return HistoryApplyPreparation{}, err
	}
	row, err := q.InsertHistoryPreparation(ctx, sqlc.InsertHistoryPreparationParams{
		ID: uuid.New(), UserID: scope.UserID, OperationID: operation, Arguments: raw, RequestHash: hash})
	if err != nil {
		return HistoryApplyPreparation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return HistoryApplyPreparation{}, err
	}
	return HistoryApplyPreparation{ID: row.ID, Arguments: a, ExpiresAt: row.ExpiresAt.Time.UTC()}, nil
}

// HistoryApplyOperation resolves an unknown outcome using the same operation ID.
// Absence is an observation, not proof that a concurrent attempt cannot commit.
func (s *Store) HistoryApplyOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (HistoryApplyReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return HistoryApplyReceipt{}, err
	}
	q := s.q.WithTx(tx)
	if err = legacyArchiveOwner(ctx, q, scope); err != nil {
		return HistoryApplyReceipt{}, err
	}
	row, err := q.GetHistoryOperation(ctx, sqlc.GetHistoryOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	var receipt HistoryApplyReceipt
	err = json.Unmarshal(row.Receipt, &receipt)
	return receipt, err
}

// ApplyHistory atomically indexes verified historical data. It creates no live
// users, registrations, payments or agent tasks. reauthorize must check IAM only;
// this transaction checks and locks the current local account itself.
func (s *Store) ApplyHistory(ctx context.Context, scope ProfileScope, r HistoryApplyRequest,
	reauthorize func(context.Context) error) (HistoryApplyReceipt, error) {
	hash, err := historyApplyHash(r.Arguments)
	if err != nil || r.OperationID == uuid.Nil || r.ProposalID == uuid.Nil || reauthorize == nil {
		return HistoryApplyReceipt{}, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("history-apply:%d:%s", scope.BotID, scope.Organization),
	); err != nil {
		return HistoryApplyReceipt{}, err
	}
	if err = legacyArchiveOwner(ctx, q, scope); err != nil {
		return HistoryApplyReceipt{}, err
	}
	prior, err := q.GetHistoryOperation(
		ctx,
		sqlc.GetHistoryOperationParams{UserID: scope.UserID, OperationID: r.OperationID},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) || prior.ProposalID != r.ProposalID {
			return HistoryApplyReceipt{}, ErrConflict
		}
		if err = finishHistoryAuthorization(ctx, tx, q, scope, reauthorize); err != nil {
			return HistoryApplyReceipt{}, err
		}
		var receipt HistoryApplyReceipt
		err = json.Unmarshal(prior.Receipt, &receipt)
		return receipt, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return HistoryApplyReceipt{}, err
	}
	prep, err := q.GetHistoryPreparation(
		ctx,
		sqlc.GetHistoryPreparationParams{UserID: scope.UserID, OperationID: r.OperationID},
	)
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	if prep.ID != r.ProposalID || !prep.Unexpired || !bytes.Equal(prep.RequestHash, hash) {
		return HistoryApplyReceipt{}, ErrConflict
	}
	raw, err := applyHistoryArchive(ctx, tx, q, scope, r.Arguments)
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	if err = finishHistoryAuthorization(ctx, tx, q, scope, reauthorize); err != nil {
		return HistoryApplyReceipt{}, err
	}
	if err = q.InsertHistoryOperation(
		ctx,
		sqlc.InsertHistoryOperationParams{UserID: scope.UserID, OperationID: r.OperationID,
			ProposalID: r.ProposalID, ArchiveID: r.Arguments.ArchiveID, RequestHash: hash, Receipt: raw},
	); err != nil {
		return HistoryApplyReceipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return HistoryApplyReceipt{}, err
	}
	var receipt HistoryApplyReceipt
	err = json.Unmarshal(raw, &receipt)
	return receipt, err
}

func applyHistoryArchive(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	scope ProfileScope,
	a HistoryApplyArguments,
) ([]byte, error) {
	archive, err := q.GetLegacyArchive(
		ctx,
		sqlc.GetLegacyArchiveParams{ID: a.ArchiveID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return nil, err
	}
	if archive.Sha256 != a.SHA256 {
		return nil, ErrConflict
	}
	raw, err := q.GetHistoryApplication(
		ctx,
		sqlc.GetHistoryApplicationParams{ArchiveID: archive.ID, ProjectionVersion: a.ProjectionVersion},
	)
	if !errors.Is(err, pgx.ErrNoRows) {
		return raw, err
	}
	receipt, err := stageAndApplyHistory(ctx, tx, q, scope, archive)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	err = q.InsertHistoryApplication(
		ctx,
		sqlc.InsertHistoryApplicationParams{
			ArchiveID:         archive.ID,
			ProjectionVersion: a.ProjectionVersion,
			Receipt:           raw,
		},
	)
	return raw, err
}

func finishHistoryAuthorization(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope ProfileScope,
	reauthorize func(context.Context) error) error {
	if err := reauthorize(ctx); err != nil {
		return err
	}
	if err := lockProfileOwner(ctx, tx, scope, false); err != nil {
		return err
	}
	return legacyArchiveOwner(ctx, q, scope)
}
