package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

const (
	legacyChunkBytes          = 256 << 10
	legacyArchiveCountLimit   = 32
	legacyArchiveStorageLimit = 1 << 30
)

var (
	ErrInvalidLegacyArchive = errors.New("invalid legacy archive storage request")
	ErrLegacyArchiveQuota   = errors.New("legacy archive storage quota reached")
)

type LegacyArchiveArguments struct {
	SHA256       string    `json:"sha256"                   minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$"`
	SourceFileID uuid.UUID `json:"source_file_id,omitempty"`
}

type LegacyArchivePreparation struct {
	ID        uuid.UUID              `json:"proposal_id"`
	Arguments LegacyArchiveArguments `json:"arguments"`
	ExpiresAt time.Time              `json:"expires_at"`
}

type LegacyArchiveRequest struct {
	OperationID uuid.UUID
	ProposalID  uuid.UUID
	Arguments   LegacyArchiveArguments
}

type LegacyArchiveReceipt struct {
	ArchiveID  uuid.UUID                     `json:"archive_id"`
	StoredAt   time.Time                     `json:"stored_at"`
	Validation legacyimport.BundleValidation `json:"validation"`
}

func legacyArchiveHash(a LegacyArchiveArguments) ([]byte, error) {
	digest, err := hex.DecodeString(a.SHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != a.SHA256 {
		return nil, ErrInvalidLegacyArchive
	}
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return hash[:], err
}

func rollbackLegacyArchive(parent context.Context, tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func legacyArchiveOwner(ctx context.Context, q *sqlc.Queries, scope ProfileScope) error {
	u, err := q.GetUserBySubject(ctx, sqlc.GetUserBySubjectParams{BotID: scope.BotID,
		ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject})
	if err != nil {
		return err
	}
	if u.ID != scope.UserID || u.Revoked {
		return pgx.ErrNoRows
	}
	return nil
}

// PrepareLegacyArchive freezes exact storage arguments. It does not accept bytes or apply history.
func (s *Store) PrepareLegacyArchive(ctx context.Context, scope ProfileScope, operation uuid.UUID,
	a LegacyArchiveArguments) (LegacyArchivePreparation, error) {
	hash, err := legacyArchiveHash(a)
	if err != nil || operation == uuid.Nil {
		return LegacyArchivePreparation{}, ErrInvalidLegacyArchive
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LegacyArchivePreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return LegacyArchivePreparation{}, err
	}
	if err = q.LockEventOperation(ctx, fmt.Sprintf("legacy-prepare:%s:%s", scope.UserID, operation)); err != nil {
		return LegacyArchivePreparation{}, err
	}
	prior, err := q.GetLegacyArchivePreparation(
		ctx,
		sqlc.GetLegacyArchivePreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return LegacyArchivePreparation{}, ErrConflict
		}
		return LegacyArchivePreparation{ID: prior.ID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LegacyArchivePreparation{}, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return LegacyArchivePreparation{}, err
	}
	row, err := q.InsertLegacyArchivePreparation(ctx, sqlc.InsertLegacyArchivePreparationParams{
		ID: uuid.New(), UserID: scope.UserID, OperationID: operation, Arguments: raw, RequestHash: hash})
	if err != nil {
		return LegacyArchivePreparation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyArchivePreparation{}, err
	}
	return LegacyArchivePreparation{ID: row.ID, Arguments: a, ExpiresAt: row.ExpiresAt.Time.UTC()}, nil
}

func legacyArchivePrior(ctx context.Context, q *sqlc.Queries, owner uuid.UUID,
	r LegacyArchiveRequest, hash []byte) (*LegacyArchiveReceipt, error) {
	row, err := q.GetLegacyArchiveOperation(
		ctx,
		sqlc.GetLegacyArchiveOperationParams{UserID: owner, OperationID: r.OperationID},
	)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(row.RequestHash, hash) || row.ProposalID != r.ProposalID {
		return nil, ErrConflict
	}
	var receipt LegacyArchiveReceipt
	if err = json.Unmarshal(row.Receipt, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

// LegacyArchiveOperation resolves an unknown outcome without refetching Telegram bytes.
func (s *Store) LegacyArchiveOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (LegacyArchiveReceipt, error) {
	if err := legacyArchiveOwner(ctx, s.q, scope); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	row, err := s.q.GetLegacyArchiveOperation(
		ctx,
		sqlc.GetLegacyArchiveOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	var result LegacyArchiveReceipt
	err = json.Unmarshal(row.Receipt, &result)
	return result, err
}

// StoreLegacyArchive commits exact compressed bytes and a stable receipt atomically.
// open must not query this store; reauthorize checks IAM only. The transaction rechecks
// and locks the local account itself after IO, including when the pool has one connection.
func (s *Store) StoreLegacyArchive(
	ctx context.Context,
	scope ProfileScope,
	r LegacyArchiveRequest,
	open func(context.Context) (io.ReadCloser, error),
	reauthorize func(context.Context) error,
) (LegacyArchiveReceipt, error) {
	hash, err := legacyArchiveHash(r.Arguments)
	if err != nil || r.OperationID == uuid.Nil || r.ProposalID == uuid.Nil || open == nil || reauthorize == nil {
		return LegacyArchiveReceipt{}, ErrInvalidLegacyArchive
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(ctx, fmt.Sprintf("legacy-store:%d:%s", scope.BotID, scope.Organization)); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if err = legacyArchiveOwner(ctx, q, scope); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	prior, err := legacyArchivePrior(ctx, q, scope.UserID, r, hash)
	if err == nil {
		return *prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LegacyArchiveReceipt{}, err
	}
	prep, err := q.GetLegacyArchivePreparation(
		ctx,
		sqlc.GetLegacyArchivePreparationParams{UserID: scope.UserID, OperationID: r.OperationID},
	)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if prep.ID != r.ProposalID || !prep.Unexpired || !bytes.Equal(prep.RequestHash, hash) {
		return LegacyArchiveReceipt{}, ErrConflict
	}
	if r.Arguments.SourceFileID != uuid.Nil {
		if err = ownedSourceFile(ctx, tx, scope.UserID, r.Arguments.SourceFileID); err != nil {
			return LegacyArchiveReceipt{}, err
		}
	}
	stored, err := s.receiveLegacyArchive(ctx, q, scope, r.Arguments, open)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if err = reauthorize(ctx); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	// A locking SELECT may have waited for an identity change; use a fresh statement.
	if err = legacyArchiveOwner(ctx, q, scope); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if err = q.InsertLegacyArchiveOperation(ctx, sqlc.InsertLegacyArchiveOperationParams{
		UserID:      scope.UserID,
		OperationID: r.OperationID,
		ProposalID:  r.ProposalID,
		ArchiveID:   stored.ArchiveID,
		RequestHash: hash,
		Receipt:     raw,
	}); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	return stored, nil
}
