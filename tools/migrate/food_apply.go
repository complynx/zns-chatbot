package migrate

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type FoodApplySummary struct {
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Events           int    `json:"events"`
	Reused           bool   `json:"reused"`
	Reconciled       bool   `json:"reconciled"`
}

func ApplyFood(ctx context.Context, dsn, stage, plan, resolutions string, limits Limits) (FoodApplySummary, error) {
	return runFoodImport(ctx, dsn, stage, plan, resolutions, limits, false)
}
func ReconcileFood(ctx context.Context, dsn, stage, plan, resolutions string, limits Limits) (FoodApplySummary, error) {
	return runFoodImport(ctx, dsn, stage, plan, resolutions, limits, true)
}

func runFoodImport(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
	verify bool,
) (FoodApplySummary, error) {
	var result FoodApplySummary
	p, err := prepareFood(stage, plan, resolutions, limits)
	if err != nil {
		return result, err
	}
	result.PlanSHA256, result.ResolutionSHA256, result.Events = p.PlanHash, p.ResolutionHash, len(p.Events)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return result, errors.New("apply_database_unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return result, errors.New("apply_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('food-import:'||$1::bigint::text,0))`,
		p.Plan.BotID,
	); err != nil {
		return result, errors.New("apply_transaction_failed")
	}
	if !verify {
		if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS migrate_import;
CREATE TABLE IF NOT EXISTS migrate_import.food_receipts(plan_sha256 text PRIMARY KEY,resolution_sha256 text NOT NULL,bot_id bigint NOT NULL,snapshot jsonb NOT NULL)`); err != nil {
			return result, errors.New("apply_schema_unavailable")
		}
	}
	var resolution string
	err = tx.QueryRow(ctx, `SELECT resolution_sha256 FROM migrate_import.food_receipts WHERE plan_sha256=$1`, p.PlanHash).
		Scan(&resolution)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, errors.New("apply_receipt_unavailable")
	}
	result.Reused = err == nil
	if result.Reused && resolution != p.ResolutionHash {
		return result, errors.New("apply_receipt_conflict")
	}
	if !result.Reused && verify {
		return result, errors.New("apply_receipt_missing")
	}
	if err = applyFoodEvents(ctx, tx, p, result.Reused); err != nil {
		return result, err
	}
	if err = recordFoodReceipt(ctx, tx, p, result.Reused); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, errors.New("apply_commit_failed")
	}
	result.Reconciled = true
	return result, nil
}

const foodSnapshotSQL = `SELECT jsonb_build_object(
'user_references',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_user_references t WHERE source_key=ANY($5)),
'event_references',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_event_references t WHERE event_id=ANY($1)),
'event_dependencies',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.pass_events t WHERE id=ANY($1)),
'event_receipts',(SELECT COALESCE(jsonb_agg(to_jsonb(t)-'receipt_snapshot' ORDER BY source_key),'[]') FROM migrate_import.event_receipts t WHERE source_key IN(SELECT source_key FROM core.legacy_event_references WHERE event_id=ANY($1))),
'user_deferrals',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_user_deferred_domains t WHERE domain='food' AND source_key=ANY($5)),
'pass_deferrals',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_pass_deferred_domains t WHERE domain='food' AND source_key IN(SELECT source_key FROM core.legacy_food_import_references WHERE event_id=ANY($1))),
'events',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY event_id),'[]') FROM core.food_events t WHERE event_id=ANY($1)),
'admins',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY event_id,owner),'[]') FROM core.food_admins t WHERE event_id=ANY($1)),
'orders',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.food_orders t WHERE event_id=ANY($1)),
'payments',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY order_id,kind,generation),'[]') FROM core.food_payments t WHERE order_id IN(SELECT id FROM core.food_orders WHERE event_id=ANY($1))),
'references',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_food_import_references t WHERE event_id=ANY($1)),
'notifications',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.food_notifications t WHERE event_id=ANY($1)),
'proofs',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.order_proofs t WHERE id IN(SELECT proof_id FROM core.food_payments WHERE order_id IN(SELECT id FROM core.food_orders WHERE event_id=ANY($1)))))`

func applyFoodEvents(ctx context.Context, tx pgx.Tx, p preparedFood, reused bool) error {
	for _, event := range p.Events {
		owners, resolveErr := resolveFoodDependencies(ctx, tx, p, event)
		if resolveErr != nil {
			return resolveErr
		}
		if !reused {
			if err := insertFoodEvent(ctx, tx, p, event, owners); err != nil {
				return err
			}
		}
	}
	if !reused {
		if err := completeFoodDeferrals(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}

func recordFoodReceipt(ctx context.Context, tx pgx.Tx, p preparedFood, reused bool) error {
	var err error
	ids := []string{}
	userKeys := []string{}
	for _, user := range p.Users {
		userKeys = append(userKeys, user.Legacy.Key)
	}
	for _, event := range p.Events {
		ids = append(ids, event.Catalog.Configuration.Event)
	}
	if reused {
		var matched bool
		err = tx.QueryRow(ctx, `SELECT snapshot=(`+foodSnapshotSQL+`) FROM migrate_import.food_receipts WHERE plan_sha256=$2 AND resolution_sha256=$3 AND bot_id=$4`, ids, p.PlanHash, p.ResolutionHash, p.Plan.BotID, userKeys).
			Scan(&matched)
		if err != nil || !matched {
			return errors.New("apply_reconciliation_failed")
		}
	} else {
		_, err = tx.Exec(
			ctx,
			`INSERT INTO migrate_import.food_receipts(plan_sha256,resolution_sha256,bot_id,snapshot) SELECT $2,$3,$4,(`+foodSnapshotSQL+`)`,
			ids,
			p.PlanHash,
			p.ResolutionHash,
			p.Plan.BotID,
			userKeys,
		)
		if err != nil {
			return errors.New("apply_receipt_conflict")
		}
	}
	return nil
}
