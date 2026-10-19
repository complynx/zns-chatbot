package db

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func archiveReceipt(row sqlc.ShuttleLegacyArchive) (LegacyArchiveReceipt, error) {
	var validation legacyimport.BundleValidation
	err := json.Unmarshal(row.Validation, &validation)
	return LegacyArchiveReceipt{ArchiveID: row.ID, StoredAt: row.CreatedAt.Time.UTC(), Validation: validation}, err
}

func (s *Store) receiveLegacyArchive(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	a LegacyArchiveArguments, open func(context.Context) (io.ReadCloser, error)) (LegacyArchiveReceipt, error) {
	row, err := q.GetLegacyArchiveByDigest(ctx, sqlc.GetLegacyArchiveByDigestParams{BotID: scope.BotID,
		OrganizationID: scope.Organization, Sha256: a.SHA256})
	if err == nil {
		return archiveReceipt(row)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LegacyArchiveReceipt{}, err
	}
	usage, err := q.LegacyArchiveUsage(
		ctx,
		sqlc.LegacyArchiveUsageParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if usage.Archives >= legacyArchiveCountLimit || usage.Bytes >= legacyArchiveStorageLimit {
		return LegacyArchiveReceipt{}, ErrLegacyArchiveQuota
	}
	input, err := open(ctx)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	defer func() { _ = input.Close() }()
	writer := legacyChunkWriter{ctx: ctx, q: q, archive: uuid.New(), buffer: make([]byte, 0, legacyChunkBytes)}
	observed := legacyInputReader{source: input}
	validation, err := legacyimport.ValidateBundle(ctx, io.TeeReader(&observed, &writer), a.SHA256)
	if ctx.Err() != nil {
		return LegacyArchiveReceipt{}, ctx.Err()
	}
	if writer.err != nil {
		return LegacyArchiveReceipt{}, writer.err
	}
	if observed.err != nil {
		return LegacyArchiveReceipt{}, observed.err
	}
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	if validation.SourceBotID != scope.BotID {
		return LegacyArchiveReceipt{}, ErrConflict
	}
	if validation.Archive.SizeBytes > legacyArchiveStorageLimit-usage.Bytes {
		return LegacyArchiveReceipt{}, ErrLegacyArchiveQuota
	}
	if err = writer.flush(); err != nil {
		return LegacyArchiveReceipt{}, err
	}
	raw, err := json.Marshal(validation)
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	row, err = q.InsertLegacyArchive(ctx, sqlc.InsertLegacyArchiveParams{ID: writer.archive,
		BotID: scope.BotID, OrganizationID: scope.Organization, Sha256: a.SHA256,
		SizeBytes: validation.Archive.SizeBytes, Validation: raw, CreatedBy: scope.UserID})
	if err != nil {
		return LegacyArchiveReceipt{}, err
	}
	return archiveReceipt(row)
}

// The validator intentionally hides source errors. Storage retains their cause
// so a temporary IO failure is not reported to Loom as permanent invalid input.
type legacyInputReader struct {
	source io.Reader
	err    error
}

func (r *legacyInputReader) Read(raw []byte) (int, error) {
	n, err := r.source.Read(raw)
	if err != nil && !errors.Is(err, io.EOF) && r.err == nil {
		r.err = err
	}
	return n, err
}

type legacyChunkWriter struct {
	ctx     context.Context
	q       *sqlc.Queries
	archive uuid.UUID
	buffer  []byte
	ordinal int32
	err     error
}

func (w *legacyChunkWriter) Write(raw []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(raw) > 0 {
		n := min(len(raw), legacyChunkBytes-len(w.buffer))
		w.buffer = append(w.buffer, raw[:n]...)
		raw = raw[n:]
		written += n
		if len(w.buffer) == legacyChunkBytes {
			if err := w.flush(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

func (w *legacyChunkWriter) flush() error {
	if w.err != nil {
		return w.err
	}
	if len(w.buffer) == 0 {
		return nil
	}
	w.err = w.q.InsertLegacyArchiveChunk(w.ctx, sqlc.InsertLegacyArchiveChunkParams{ArchiveID: w.archive,
		Ordinal: w.ordinal, Content: w.buffer})
	if w.err != nil {
		return w.err
	}
	w.ordinal++
	w.buffer = w.buffer[:0]
	return nil
}
