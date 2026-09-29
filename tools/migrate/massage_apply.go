package migrate

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func timeDurationSlots(slot int) time.Duration { return time.Duration(slot) * 20 * time.Minute }

type MassageApplySummary struct {
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Applied          int    `json:"applied"`
	Reused           bool   `json:"reused"`
	Reconciled       bool   `json:"reconciled"`
}

func ApplyMassage(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
) (MassageApplySummary, error) {
	return runMassageImport(ctx, dsn, stage, plan, resolutions, limits, false)
}

func ReconcileMassage(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
) (MassageApplySummary, error) {
	return runMassageImport(ctx, dsn, stage, plan, resolutions, limits, true)
}

func runMassageImport(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
	verify bool,
) (MassageApplySummary, error) {
	var summary MassageApplySummary
	p, err := prepareMassage(stage, plan, resolutions, limits)
	if err != nil {
		return summary, err
	}
	summary.PlanSHA256, summary.ResolutionSHA256 = p.PlanHash, p.ResolutionHash
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return summary, errors.New("apply_database_unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return summary, errors.New("apply_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('migrate_import.user_receipts',0))`,
	); err != nil {
		return summary, errors.New("apply_transaction_failed")
	}
	if !verify {
		if _, err = tx.Exec(
			ctx,
			`CREATE SCHEMA IF NOT EXISTS migrate_import; CREATE TABLE IF NOT EXISTS migrate_import.massage_receipts(manifest_sha256 text PRIMARY KEY, plan_sha256 text NOT NULL,resolution_sha256 text NOT NULL,owners jsonb NOT NULL,snapshot jsonb NOT NULL)`,
		); err != nil {
			return summary, errors.New("apply_schema_unavailable")
		}
	}
	if _, err = tx.Exec(
		ctx,
		`LOCK TABLE core.massage_events,core.massage_parties,core.massage_specialists,core.massage_work,core.massage_bookings,core.massage_notices,core.legacy_massage_import_references,core.legacy_massage_drafts,core.legacy_user_deferred_domains IN SHARE ROW EXCLUSIVE MODE`,
	); err != nil {
		return summary, errors.New("apply_lock_failed")
	}
	owners, err := resolveMassageDependencies(ctx, tx, p)
	if err != nil {
		return summary, err
	}
	reused, err := massageReceipt(ctx, tx, p, owners, verify)
	if err != nil {
		return summary, err
	}
	if !reused {
		summary.Applied = len(p.Plan.Records)
	}
	if tx.Commit(ctx) != nil {
		return summary, errors.New("apply_commit_failed")
	}
	summary.Reused, summary.Reconciled = reused, true
	return summary, nil
}
func resolveMassageDependencies(ctx context.Context, tx pgx.Tx, p preparedMassage) (map[int64]string, error) {
	for _, event := range p.eventNames() {
		d := p.Events[event]
		var found string
		if err := tx.QueryRow(ctx, `SELECT r.event_id FROM core.legacy_event_references r JOIN core.pass_events e ON e.id=r.event_id WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND r.event_id=$3 FOR SHARE OF r,e`, d.Legacy.Key, d.Legacy.RecordSHA256, event).
			Scan(&found); err != nil {
			return nil, errors.New("apply_event_identity_unresolved")
		}
	}
	ids := map[int64]bool{}
	for id := range p.Specialists {
		ids[id] = true
	}
	for _, row := range p.Plan.Records {
		if row.Booking != nil && !row.Excluded {
			ids[row.Booking.Owner] = true
			if row.Booking.Specialist > 0 {
				ids[row.Booking.Specialist] = true
			}
		}
	}
	owners := map[int64]string{}
	for id := range ids {
		d := p.Users[id]
		var owner string
		if err := tx.QueryRow(ctx, `SELECT r.owner FROM core.legacy_user_references r JOIN core.telegram_identities t ON t.owner=r.owner JOIN core.users u ON u.id=r.owner WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND t.bot_id=$3 AND t.telegram_id=$4 AND u.telegram_id=$4 FOR SHARE OF r,t,u`, d.Legacy.Key, d.Legacy.RecordSHA256, p.Plan.BotID, id).
			Scan(&owner); err != nil {
			return nil, errors.New("apply_owner_identity_unresolved")
		}
		owners[id] = owner
	}
	return owners, nil
}

func massageReceipt(
	ctx context.Context,
	tx pgx.Tx,
	p preparedMassage,
	owners map[int64]string,
	verify bool,
) (bool, error) {
	var err error
	var planHash, resolutionHash string
	err = tx.QueryRow(ctx, `SELECT plan_sha256,resolution_sha256 FROM migrate_import.massage_receipts WHERE manifest_sha256=$1`, p.Plan.ManifestSHA256).
		Scan(&planHash, &resolutionHash)
	reused := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, errors.New("apply_receipt_unavailable")
	}
	if reused {
		if planHash != p.PlanHash || resolutionHash != p.ResolutionHash {
			return false, errors.New("apply_receipt_conflict")
		}
		var matched bool
		if err = tx.QueryRow(ctx, `SELECT owners=$2::jsonb AND snapshot=(`+massageSnapshotSQL+`) FROM migrate_import.massage_receipts WHERE manifest_sha256=$3`, p.eventNames(), owners, p.Plan.ManifestSHA256, p.Plan.BotID).
			Scan(&matched); err != nil ||
			!matched {
			return false, errors.New("apply_reconciliation_failed")
		}

		return true, nil
	}

	if verify {
		return false, errors.New("apply_receipt_missing")
	}
	if err = insertMassageStage(ctx, tx, p, owners); err != nil {
		return false, err
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO migrate_import.massage_receipts SELECT $3,$5,$6,$2,(`+massageSnapshotSQL+`)`,
		p.eventNames(),
		owners,
		p.Plan.ManifestSHA256,
		p.Plan.BotID,
		p.PlanHash,
		p.ResolutionHash,
	); err != nil {
		return false, errors.New("apply_receipt_conflict")
	}

	return reused, nil
}
