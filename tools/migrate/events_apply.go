package migrate

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// EventApplySummary reports committed event transactions, including partial runs.
type EventApplySummary struct {
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Candidates       int64  `json:"candidates"`
	Applied          int64  `json:"applied"`
	Reused           int64  `json:"reused"`
	Reconciled       bool   `json:"reconciled"`
}

func ApplyEvents(ctx context.Context, dsn, stage, plan, resolutions string, limits Limits) (EventApplySummary, error) {
	var summary EventApplySummary
	prepared, err := prepareEvents(stage, plan, resolutions, limits)
	if err != nil {
		return summary, err
	}
	summary = EventApplySummary{
		PlanSHA256:       prepared.planHash,
		ResolutionSHA256: prepared.resolutionHash,
		Candidates:       int64(len(prepared.plan.Events)),
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return summary, errors.New("apply_database_unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if err = prepareEventReceipts(ctx, conn); err != nil {
		return summary, err
	}
	for _, row := range prepared.plan.Events {
		reused, applyErr := applyOneEvent(ctx, conn, prepared, row)
		if applyErr != nil {
			return summary, applyErr
		}
		if reused {
			summary.Reused++
		} else {
			summary.Applied++
		}
	}
	summary.Reconciled = true
	return summary, nil
}

func prepareEventReceipts(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return errors.New("apply_schema_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Shared schema creation uses the same catalog lock as the users importer.
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('migrate_import.user_receipts',0))`,
	); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS migrate_import;
CREATE TABLE IF NOT EXISTS migrate_import.event_receipts (
 source_key text PRIMARY KEY REFERENCES core.legacy_event_references(source_key),
 plan_sha256 text NOT NULL, resolution_sha256 text NOT NULL, admin_sha256 text NOT NULL, event_snapshot jsonb, receipt_snapshot jsonb
);
ALTER TABLE migrate_import.event_receipts ADD COLUMN IF NOT EXISTS event_snapshot jsonb;
ALTER TABLE migrate_import.event_receipts ADD COLUMN IF NOT EXISTS receipt_snapshot jsonb`); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	return nil
}

func applyOneEvent(ctx context.Context, conn *pgx.Conn, plan preparedEvents, row EventPlanRecord) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, errors.New("apply_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, row.Legacy.Key); err != nil {
		return false, errors.New("apply_transaction_failed")
	}
	var planHash, resolutionHash, adminHash string
	err = tx.QueryRow(ctx, `SELECT plan_sha256,resolution_sha256,admin_sha256 FROM migrate_import.event_receipts WHERE source_key=$1 FOR UPDATE`, row.Legacy.Key).
		Scan(&planHash, &resolutionHash, &adminHash)
	reused := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, errors.New("apply_receipt_unavailable")
	}
	if reused && (planHash != plan.planHash || resolutionHash != plan.resolutionHash) {
		return false, errors.New("apply_receipt_conflict")
	}
	ownersHash, err := eventAdminOwnersHash(ctx, tx, plan.plan.BotID, row.Candidate)
	if err != nil {
		return false, err
	}
	if reused && adminHash != ownersHash {
		return false, errors.New("apply_reconciliation_failed")
	}
	if !reused {
		if err = insertEvent(ctx, tx, plan, row, ownersHash); err != nil {
			return false, err
		}
	}
	var lockedEvent string
	if err = tx.QueryRow(ctx, `SELECT id FROM core.pass_events WHERE id=$1 FOR SHARE`, row.Candidate.ID).
		Scan(&lockedEvent); err != nil {
		return false, errors.New("apply_event_dependency_unavailable")
	}
	if err = reconcileEvent(ctx, tx, plan, row); err != nil {
		return false, err
	}
	// A legacy receipt gets a baseline only after original resolutions reconcile.
	if err = recordEventDependencySnapshot(ctx, tx, row.Candidate.ID, row.Legacy.Key); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, errors.New("apply_commit_failed")
	}
	return reused, nil
}

func insertEvent(ctx context.Context, tx pgx.Tx, plan preparedEvents, row EventPlanRecord, ownersHash string) error {
	event := row.Candidate
	titles, err := json.Marshal(event.Titles)
	if err != nil {
		return errors.New("event_title_invalid")
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_events(id,finishes_at,passport_required,assignment_rule,disable_concurrency_limit,titles,display_order) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		event.ID,
		event.FinishesAt,
		event.PassportRequired,
		event.AssignmentRule,
		event.DisableConcurrencyLimit,
		titles,
		plan.orders[row.Legacy.Key],
	); err != nil {
		return errors.New("apply_target_conflict")
	}
	if err = insertEventConfiguration(ctx, tx, event); err != nil {
		return err
	}
	for position, tier := range event.Tiers {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at,promo,blocked_by_date) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			event.ID,
			position,
			tier.Amount,
			tier.Price,
			tier.StartsAt,
			tier.Promo,
			tier.BlockedByDate,
		); err != nil {
			return errors.New("apply_tier_conflict")
		}
	}
	for _, admin := range event.Admins {
		tag, insertErr := tx.Exec(
			ctx,
			`INSERT INTO core.pass_payment_admins(event_id,owner,hidden) SELECT $1,t.owner,$2 FROM core.telegram_identities t JOIN core.users u ON u.id=t.owner WHERE t.bot_id=$3 AND t.telegram_id=$4 AND u.telegram_id=$4`,
			event.ID,
			admin.Hidden,
			plan.plan.BotID,
			admin.TelegramID,
		)
		if insertErr != nil || tag.RowsAffected() != 1 {
			return errors.New("apply_admin_identity_unresolved")
		}
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.legacy_event_references(source_key,event_id,source_record_sha256) VALUES($1,$2,$3)`,
		row.Legacy.Key,
		event.ID,
		row.Legacy.RecordSHA256,
	); err != nil {
		return errors.New("apply_legacy_conflict")
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO migrate_import.event_receipts(source_key,plan_sha256,resolution_sha256,admin_sha256) VALUES($1,$2,$3,$4)`,
		row.Legacy.Key,
		plan.planHash,
		plan.resolutionHash,
		ownersHash,
	); err != nil {
		return errors.New("apply_receipt_conflict")
	}
	return nil
}

func eventAdminOwnersHash(ctx context.Context, tx pgx.Tx, botID int64, event *EventCandidate) (string, error) {
	owners := make([]string, 0, len(event.Admins))
	for _, admin := range event.Admins {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT t.owner FROM core.telegram_identities t JOIN core.users u ON u.id=t.owner WHERE t.bot_id=$1 AND t.telegram_id=$2 AND u.telegram_id=$2`, botID, admin.TelegramID).
			Scan(&owner); err != nil {
			return "", errors.New("apply_admin_identity_unresolved")
		}
		owners = append(owners, owner)
	}
	raw, err := json.Marshal(owners)
	if err != nil {
		return "", errors.New("apply_admin_identity_unresolved")
	}
	return hashBytes(raw), nil
}

func recordEventDependencySnapshot(ctx context.Context, tx pgx.Tx, event, key string) error {
	var current []byte
	if err := tx.QueryRow(ctx, `SELECT to_jsonb(e) FROM core.pass_events e WHERE id=$1 FOR SHARE`, event).
		Scan(&current); err != nil {
		return errors.New("apply_event_dependency_unavailable")
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE migrate_import.event_receipts SET event_snapshot=COALESCE(event_snapshot,$2::jsonb),
 receipt_snapshot=COALESCE(receipt_snapshot,jsonb_build_object('source_key',source_key,'plan_sha256',plan_sha256,'resolution_sha256',resolution_sha256,'admin_sha256',admin_sha256))
 WHERE source_key=$1`,
		key,
		current,
	); err != nil {
		return errors.New("apply_reconciliation_failed")
	}
	var matched bool
	if err := tx.QueryRow(ctx, `SELECT event_snapshot=$2::jsonb AND receipt_snapshot=jsonb_build_object('source_key',source_key,'plan_sha256',plan_sha256,'resolution_sha256',resolution_sha256,'admin_sha256',admin_sha256)
 FROM migrate_import.event_receipts WHERE source_key=$1`, key, current).
		Scan(&matched); err != nil ||
		!matched {
		return errors.New("apply_reconciliation_failed")
	}
	return nil
}
