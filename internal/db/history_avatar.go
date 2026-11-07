package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

const historyAvatarValidationTimeout = 30 * time.Second

// HistoryAvatarSource contains an original Telegram input reference, not an
// archived image or a restored historical crop. File IDs are server-only.
type HistoryAvatarSource struct {
	legacyimport.AvatarInput
	HistoryRegistrationProvenance
}

// OwnerHistoryAvatarSource validates the applied archive and current Telegram
// binding before returning a reference. reauthorize must not use this DB pool.
// Callers must recheck authorization after downloading from Telegram.
func (s *Store) OwnerHistoryAvatarSource(ctx context.Context, scope ProfileScope,
	recordID, archiveID uuid.UUID, reauthorize func(context.Context) error,
) (HistoryAvatarSource, error) {
	if recordID == uuid.Nil || archiveID == uuid.Nil || reauthorize == nil {
		return HistoryAvatarSource{}, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	row, err := q.OwnerHistoryAvatarSource(ctx, sqlc.OwnerHistoryAvatarSourceParams{
		ID: recordID, ID_2: archiveID, BotID: scope.BotID,
		OrganizationID: scope.Organization, OwnerTelegramID: owner,
	})
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	result, err := readHistoryAvatarSource(ctx, q, row)
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	if err = reauthorize(ctx); err != nil {
		return HistoryAvatarSource{}, err
	}
	currentOwner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	if currentOwner != owner {
		return HistoryAvatarSource{}, ErrConflict
	}
	return result, nil
}

func readHistoryAvatarSource(ctx context.Context, q *sqlc.Queries,
	row sqlc.OwnerHistoryAvatarSourceRow,
) (HistoryAvatarSource, error) {
	ctx, cancel := context.WithTimeout(ctx, historyAvatarValidationTimeout)
	defer cancel()
	record, archive := row.ShuttleLegacyHistoryRecord, row.ShuttleLegacyArchive
	if record.ProjectionVersion != legacyimport.ProjectionVersion {
		return HistoryAvatarSource{}, ErrConflict
	}
	reader, err := newLegacyArchiveReader(ctx, q, archive)
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	target := legacyimport.Record{
		SourceInstance: record.SourceInstance, Collection: record.CollectionName,
		SourceKey: record.SourceKey, Role: record.SourceRole, OwnerID: record.OwnerTelegramID,
		Line: int(row.SourceLine),
	}
	copy(target.SHA256[:], record.CanonicalHash)
	observed := legacyInputReader{source: reader}
	input, validation, err := legacyimport.ReadAvatarInput(ctx, &observed, archive.Sha256, target)
	if ctx.Err() != nil {
		return HistoryAvatarSource{}, ctx.Err()
	}
	if observed.err != nil {
		return HistoryAvatarSource{}, observed.err
	}
	if errors.Is(err, legacyimport.ErrSourceRecord) {
		return HistoryAvatarSource{}, ErrConflict
	}
	if err != nil {
		return HistoryAvatarSource{}, err
	}
	if validation.SourceBotID != record.BotID || validation.Archive.SizeBytes != archive.SizeBytes {
		return HistoryAvatarSource{}, ErrConflict
	}
	provenance := HistoryRegistrationProvenance{
		RecordID: record.ID, ArchiveID: archive.ID, ArchiveSHA256: archive.Sha256,
		SourceInstance: record.SourceInstance, Collection: record.CollectionName,
		SourceKey: record.SourceKey, SourceLine: row.SourceLine,
	}
	return HistoryAvatarSource{AvatarInput: input, HistoryRegistrationProvenance: provenance}, nil
}
