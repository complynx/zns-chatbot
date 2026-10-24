package db

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

type HistoryRegistration struct {
	legacyimport.RegistrationClaim
	HistoryRegistrationProvenance
}

type HistoryRegistrationProvenance struct {
	RecordID       uuid.UUID `json:"record_id"`
	ArchiveID      uuid.UUID `json:"archive_id"`
	ArchiveSHA256  string    `json:"archive_sha256"`
	SourceInstance string    `json:"source_instance"`
	Collection     string    `json:"collection"`
	SourceKey      string    `json:"source_key"`
	SourceLine     int32     `json:"source_line"`
}

type HistoryRegistrations struct {
	legacyimport.RegistrationPage
	HistoryRegistrationProvenance
}

// OwnerHistoryRegistration reads one explicit historical field while holding the
// current owner's account lock. reauthorize must use IAM only, not this DB pool.
func (s *Store) OwnerHistoryRegistration(ctx context.Context, scope ProfileScope, id uuid.UUID, key string,
	reauthorize func(context.Context) error,
) (HistoryRegistration, error) {
	if !legacyimport.ValidRegistrationKey(key) {
		return HistoryRegistration{}, ErrInvalidHistory
	}
	var claim legacyimport.RegistrationClaim
	provenance, err := s.readOwnerRegistration(
		ctx,
		scope,
		id,
		reauthorize,
		func(
			ctx context.Context,
			input io.Reader,
			digest string,
			target legacyimport.Record,
		) (legacyimport.BundleValidation, error) {
			var validation legacyimport.BundleValidation
			var readErr error
			claim, validation, readErr = legacyimport.ReadRegistration(ctx, input, digest, target, key)
			return validation, readErr
		},
	)
	if err != nil {
		return HistoryRegistration{}, err
	}
	return HistoryRegistration{RegistrationClaim: claim, HistoryRegistrationProvenance: provenance}, nil
}

// OwnerHistoryRegistrations lists supported historical candidates using the same
// locked source and final IAM check as the explicit-key read.
func (s *Store) OwnerHistoryRegistrations(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
	after string,
	limit int,
	reauthorize func(context.Context) error,
) (HistoryRegistrations, error) {
	if limit == 0 {
		limit = 10
	}
	if (after != "" && !legacyimport.ValidRegistrationKey(after)) || limit < 1 ||
		limit > legacyimport.MaxRegistrationPageSize {
		return HistoryRegistrations{}, ErrInvalidHistory
	}
	var page legacyimport.RegistrationPage
	provenance, err := s.readOwnerRegistration(
		ctx,
		scope,
		id,
		reauthorize,
		func(
			ctx context.Context,
			input io.Reader,
			digest string,
			target legacyimport.Record,
		) (legacyimport.BundleValidation, error) {
			var validation legacyimport.BundleValidation
			var readErr error
			page, validation, readErr = legacyimport.ReadRegistrations(ctx, input, digest, target, after, limit)
			return validation, readErr
		},
	)
	if err != nil {
		return HistoryRegistrations{}, err
	}
	return HistoryRegistrations{RegistrationPage: page, HistoryRegistrationProvenance: provenance}, nil
}

func (s *Store) readOwnerRegistration(ctx context.Context, scope ProfileScope, id uuid.UUID,
	reauthorize func(context.Context) error,
	read func(context.Context, io.Reader, string, legacyimport.Record) (legacyimport.BundleValidation, error),
) (HistoryRegistrationProvenance, error) {
	if id == uuid.Nil || reauthorize == nil {
		return HistoryRegistrationProvenance{}, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	row, err := q.OwnerHistoryRegistrationSource(
		ctx,
		sqlc.OwnerHistoryRegistrationSourceParams{
			ID:              id,
			BotID:           scope.BotID,
			OrganizationID:  scope.Organization,
			OwnerTelegramID: owner,
		},
	)
	if err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	record, archive := row.ShuttleLegacyHistoryRecord, row.ShuttleLegacyArchive
	if record.ProjectionVersion != legacyimport.ProjectionVersion {
		return HistoryRegistrationProvenance{}, ErrConflict
	}
	reader, err := newLegacyArchiveReader(ctx, q, archive)
	if err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	target := legacyimport.Record{
		SourceInstance: record.SourceInstance,
		Collection:     record.CollectionName,
		SourceKey:      record.SourceKey,
		Role:           record.SourceRole,
		OwnerID:        owner,
		Line:           int(record.FirstLine),
	}
	copy(target.SHA256[:], record.CanonicalHash)
	observed := legacyInputReader{source: reader}
	validation, err := read(ctx, &observed, archive.Sha256, target)
	if ctx.Err() != nil {
		return HistoryRegistrationProvenance{}, ctx.Err()
	}
	if observed.err != nil {
		return HistoryRegistrationProvenance{}, observed.err
	}
	if errors.Is(err, legacyimport.ErrRegistrationSource) {
		return HistoryRegistrationProvenance{}, ErrConflict
	}
	if err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	if validation.SourceBotID != scope.BotID || validation.Archive.SizeBytes != archive.SizeBytes {
		return HistoryRegistrationProvenance{}, ErrConflict
	}
	if err = reauthorize(ctx); err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	currentOwner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return HistoryRegistrationProvenance{}, err
	}
	if currentOwner != owner {
		return HistoryRegistrationProvenance{}, ErrConflict
	}
	return HistoryRegistrationProvenance{
		RecordID:       id,
		ArchiveID:      archive.ID,
		ArchiveSHA256:  archive.Sha256,
		SourceInstance: record.SourceInstance,
		Collection:     record.CollectionName,
		SourceKey:      record.SourceKey,
		SourceLine:     record.FirstLine,
	}, nil
}
