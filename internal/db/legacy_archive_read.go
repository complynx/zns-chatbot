package db

import (
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

// ValidateStoredLegacyArchive reads one bounded chunk at a time and recomputes
// validation from stored bytes. It does not trust the saved inspection JSON.
func (s *Store) ValidateStoredLegacyArchive(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
) (legacyimport.BundleValidation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return legacyimport.BundleValidation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = legacyArchiveOwner(ctx, q, scope); err != nil {
		return legacyimport.BundleValidation{}, err
	}
	row, err := q.GetLegacyArchive(
		ctx,
		sqlc.GetLegacyArchiveParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return legacyimport.BundleValidation{}, err
	}
	reader, err := newLegacyArchiveReader(ctx, q, row)
	if err != nil {
		return legacyimport.BundleValidation{}, err
	}
	observed := legacyInputReader{source: reader}
	validation, err := legacyimport.ValidateBundle(ctx, &observed, row.Sha256)
	if ctx.Err() != nil {
		return legacyimport.BundleValidation{}, ctx.Err()
	}
	if observed.err != nil {
		return legacyimport.BundleValidation{}, observed.err
	}
	if err != nil {
		return legacyimport.BundleValidation{}, err
	}
	if validation.SourceBotID != scope.BotID || validation.Archive.SizeBytes != row.SizeBytes {
		return legacyimport.BundleValidation{}, ErrConflict
	}
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return legacyimport.BundleValidation{}, err
	}
	if err = legacyArchiveOwner(ctx, q, scope); err != nil {
		return legacyimport.BundleValidation{}, err
	}
	return validation, nil
}

// QueryRow finishes each result before returning its bytes. A future visitor can
// therefore issue inserts on this same transaction without an open pgx.Rows stream.
type legacyArchiveReader struct {
	ctx      context.Context
	q        *sqlc.Queries
	id       uuid.UUID
	size     int64
	position int64
	ordinal  int32
	buffer   []byte
}

func newLegacyArchiveReader(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ShuttleLegacyArchive,
) (*legacyArchiveReader, error) {
	stats, err := q.LegacyArchiveChunkStats(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	expected := (row.SizeBytes + legacyChunkBytes - 1) / legacyChunkBytes
	if row.SizeBytes <= 0 || row.SizeBytes > legacyimport.MaxBundleBytes ||
		stats.Chunks != expected || stats.Bytes != row.SizeBytes ||
		stats.FirstOrdinal != 0 ||
		int64(stats.LastOrdinal) != expected-1 {
		return nil, legacyimport.ErrInvalidBundle
	}
	return &legacyArchiveReader{ctx: ctx, q: q, id: row.ID, size: row.SizeBytes}, nil
}

func (r *legacyArchiveReader) Read(raw []byte) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.position == r.size {
		return 0, io.EOF
	}
	if len(r.buffer) == 0 {
		buffer, err := r.q.GetLegacyArchiveChunk(
			r.ctx,
			sqlc.GetLegacyArchiveChunkParams{ArchiveID: r.id, Ordinal: r.ordinal},
		)
		if err != nil {
			return 0, err
		}
		if int64(len(buffer)) != min(int64(legacyChunkBytes), r.size-r.position) {
			return 0, legacyimport.ErrInvalidBundle
		}
		r.buffer = buffer
		r.ordinal++
	}
	n := copy(raw, r.buffer)
	r.buffer = r.buffer[n:]
	r.position += int64(n)
	return n, nil
}
