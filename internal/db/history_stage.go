package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

const (
	historyBatchBytes      = 1 << 20
	historyBatchRecords    = 100
	historyRecordLimit     = 1_000_000
	historyProjectionLimit = 1 << 30
	historyRecordOverhead  = 128
)

// Batches finish reading a database chunk before COPY starts on the same
// connection. One source record may exceed the batch byte budget; it is flushed
// alone and remains bounded by the importer's source-record limit.
type historyStage struct {
	tx                 pgx.Tx
	rows               [][]any
	bytes              int
	records, projected int64
}

func (b *historyStage) add(ctx context.Context, r legacyimport.Record) error {
	size := len(r.Projection) + len(r.SourceKey) + len(r.SourceInstance) + len(r.Collection) + historyRecordOverhead
	if len(b.rows) > 0 && (len(b.rows) >= historyBatchRecords || b.bytes+size > historyBatchBytes) {
		if err := b.flush(ctx); err != nil {
			return err
		}
	}
	var projection any
	if r.Projection != nil {
		projection = []byte(r.Projection)
		b.projected++
	}
	b.rows = append(b.rows, []any{uuid.New(), r.SourceInstance, r.Collection, r.SourceKey,
		r.Role, r.OwnerID, r.SHA256[:], projection, r.Line})
	b.bytes += size
	b.records++
	if b.bytes >= historyBatchBytes || len(b.rows) >= historyBatchRecords {
		return b.flush(ctx)
	}
	return nil
}

func (b *historyStage) flush(ctx context.Context) error {
	if len(b.rows) == 0 {
		return nil
	}
	_, err := b.tx.CopyFrom(
		ctx,
		pgx.Identifier{"pg_temp", "zinusya_history_stage"},
		[]string{
			"id",
			"source_instance",
			"collection_name",
			"source_key",
			"source_role",
			"owner_telegram_id",
			"canonical_hash",
			"projection",
			"source_line",
		},
		pgx.CopyFromRows(b.rows),
	)
	b.rows = nil
	b.bytes = 0
	return err
}

func stageAndApplyHistory(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope ProfileScope,
	archive sqlc.ShuttleLegacyArchive) (HistoryApplyReceipt, error) {
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE zinusya_history_stage (
id uuid NOT NULL, source_instance text NOT NULL, collection_name text NOT NULL,
source_key text NOT NULL, source_role text NOT NULL, owner_telegram_id bigint NOT NULL,
canonical_hash bytea NOT NULL, projection jsonb, source_line integer NOT NULL,
PRIMARY KEY(source_instance,collection_name,source_key)) ON COMMIT DROP`); err != nil {
		return HistoryApplyReceipt{}, err
	}
	reader, err := newLegacyArchiveReader(ctx, q, archive)
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	observed := legacyInputReader{source: reader}
	batch := historyStage{tx: tx}
	validation, err := legacyimport.VisitBundle(
		ctx,
		&observed,
		archive.Sha256,
		func(r legacyimport.Record) error { return batch.add(ctx, r) },
	)
	if ctx.Err() != nil {
		return HistoryApplyReceipt{}, ctx.Err()
	}
	if observed.err != nil {
		return HistoryApplyReceipt{}, observed.err
	}
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	if validation.SourceBotID != scope.BotID || validation.Archive.SizeBytes != archive.SizeBytes {
		return HistoryApplyReceipt{}, ErrConflict
	}
	if err = batch.flush(ctx); err != nil {
		return HistoryApplyReceipt{}, err
	}
	if err = historyConflicts(ctx, tx, scope); err != nil {
		return HistoryApplyReceipt{}, err
	}
	inserted, err := tx.Exec(ctx, "INSERT INTO shuttle_legacy_history_records\n(id,bot_id,organization_id,so" +
"urce_instance,collection_name,source_key,source_role,\nowner_telegram_id," +
"canonical_hash,projection_version,projection,first_archive_id,first_line" +
")\nSELECT id,$1,$2,source_instance,collection_name,source_key,source_role" +
",owner_telegram_id,\ncanonical_hash,$3,projection,$4,source_line FROM pg_" +
"temp.zinusya_history_stage\nON CONFLICT(bot_id,organization_id,source_ins" +
"tance,collection_name,source_key) DO NOTHING", scope.BotID, scope.Organization, legacyimport.ProjectionVersion, archive.ID)
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	// Check again after ON CONFLICT, including a writer that bypassed our advisory lock.
	if err = historyConflicts(ctx, tx, scope); err != nil {
		return HistoryApplyReceipt{}, err
	}
	usage, err := q.HistoryUsage(ctx, sqlc.HistoryUsageParams{BotID: scope.BotID, OrganizationID: scope.Organization})
	if err != nil {
		return HistoryApplyReceipt{}, err
	}
	if usage.Records > historyRecordLimit || usage.Bytes > historyProjectionLimit {
		return HistoryApplyReceipt{}, ErrHistoryQuota
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_legacy_history_memberships(archive_id,record_id,source_role,source_line)
SELECT $1,h.id,s.source_role,s.source_line FROM pg_temp.zinusya_history_stage s
JOIN shuttle_legacy_history_records h ON h.bot_id=$2 AND h.organization_id=$3
AND h.source_instance=s.source_instance AND h.collection_name=s.collection_name AND h.source_key=s.source_key`,
		archive.ID,
		scope.BotID,
		scope.Organization,
	); err != nil {
		return HistoryApplyReceipt{}, err
	}
	return HistoryApplyReceipt{
		ArchiveID:         archive.ID,
		SHA256:            archive.Sha256,
		ProjectionVersion: legacyimport.ProjectionVersion,
		AppliedAt:         time.Now().UTC(),
		Records:           batch.records,
		Projected:         batch.projected,
		Inserted:          inserted.RowsAffected(),
		Reused:            batch.records - inserted.RowsAffected(),
	}, nil
}

func historyConflicts(ctx context.Context, tx pgx.Tx, scope ProfileScope) error {
	var conflict bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_temp.zinusya_history_stage s\nJOIN shuttle" +
"_legacy_history_records h ON h.bot_id=$1 AND h.organization_id=$2\nAND h." +
"source_instance=s.source_instance AND h.collection_name=s.collection_nam" +
"e AND h.source_key=s.source_key\nWHERE h.source_role IS DISTINCT FROM s.s" +
"ource_role OR h.owner_telegram_id IS DISTINCT FROM s.owner_telegram_id\nO" +
"R h.canonical_hash IS DISTINCT FROM s.canonical_hash OR h.projection_ver" +
"sion IS DISTINCT FROM $3::text)", scope.BotID, scope.Organization, legacyimport.ProjectionVersion).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return ErrConflict
	}
	return nil
}
