package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type OrderApplySummary struct {
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Events           int64  `json:"events"`
	Applied          int64  `json:"applied"`
	Reused           int64  `json:"reused"`
	Reconciled       bool   `json:"reconciled"`
}

// ApplyOrders commits the complete domain. No runtime capacity
// reconciler is invoked and no existing target event is merged or overwritten.
func ApplyOrders(
	ctx context.Context,
	dsn, stage, planPath, resolutions string,
	limits Limits,
) (OrderApplySummary, error) {
	return runOrderImport(ctx, dsn, stage, planPath, resolutions, limits, false)
}

// ReconcileOrders verifies receipts and exact target state without inserting or
// updating any business rows, references or receipt schema.
func ReconcileOrders(
	ctx context.Context,
	dsn, stage, planPath, resolutions string,
	limits Limits,
) (OrderApplySummary, error) {
	return runOrderImport(ctx, dsn, stage, planPath, resolutions, limits, true)
}

func runOrderImport(
	ctx context.Context,
	dsn, stage, planPath, resolutions string,
	limits Limits,
	verifyOnly bool,
) (OrderApplySummary, error) {
	var summary OrderApplySummary
	prepared, err := prepareOrders(stage, planPath, resolutions, limits)
	if err != nil {
		return summary, err
	}
	summary = OrderApplySummary{
		PlanSHA256:       prepared.PlanHash,
		ResolutionSHA256: prepared.ResolutionHash,
		Events:           int64(len(prepared.Events)),
	}
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
	if !verifyOnly {
		err = prepareOrderReceipts(ctx, tx)
	}
	if err != nil {
		return summary, err
	}
	for _, event := range prepared.Events {
		if err = applyOrderEvent(ctx, tx, prepared, event, verifyOnly); err != nil {
			return summary, err
		}
	}
	if tx.Commit(ctx) != nil {
		return summary, errors.New("apply_commit_failed")
	}
	if verifyOnly {
		summary.Reused = summary.Events
	} else {
		summary.Applied = summary.Events
	}
	summary.Reconciled = true
	return summary, nil
}
func prepareOrderReceipts(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('migrate_import.user_receipts',0))`,
	); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	if _, err := tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS migrate_import;
CREATE TABLE IF NOT EXISTS migrate_import.order_receipts (
 source_key text PRIMARY KEY REFERENCES core.legacy_order_import_references(source_key),
 plan_sha256 text NOT NULL,resolution_sha256 text NOT NULL,owners jsonb NOT NULL,snapshot jsonb NOT NULL
)`); err != nil {
		return errors.New("apply_schema_unavailable")
	}
	return nil
}

func applyOrderEvent(
	ctx context.Context,
	tx pgx.Tx,
	p preparedOrders,
	event preparedOrderEvent,
	verifyOnly bool,
) error {
	key := event.Catalog.Legacy.Key
	owners, err := resolveOrderDependencies(ctx, tx, p, event)
	if err != nil {
		return err
	}
	if verifyOnly {
		return reconcileImportedOrderEvent(ctx, tx, p, event, owners)
	}
	if err = insertOrderEvent(ctx, tx, p, event, owners); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO migrate_import.order_receipts(source_key,plan_sha256,resolution_sha256,owners,snapshot)
	SELECT $2,$3,$4,$5,(`+orderSnapshotSQL+`)`,
		event.Catalog.Catalog.EventID,
		key,
		p.PlanHash,
		p.ResolutionHash,
		owners,
	)
	if err != nil {
		return errors.New("apply_receipt_conflict")
	}
	return nil
}

func reconcileImportedOrderEvent(
	ctx context.Context,
	tx pgx.Tx,
	p preparedOrders,
	event preparedOrderEvent,
	owners map[string]string,
) error {
	var bound, matched bool
	err := tx.QueryRow(ctx, `SELECT plan_sha256=$4 AND resolution_sha256=$5,owners=$2::jsonb AND snapshot=(`+orderSnapshotSQL+`) FROM migrate_import.order_receipts WHERE source_key=$3`, event.Catalog.Catalog.EventID, owners, event.Catalog.Legacy.Key, p.PlanHash, p.ResolutionHash).
		Scan(&bound, &matched)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("apply_receipt_missing")
	}
	if err != nil {
		return errors.New("apply_receipt_unavailable")
	}
	if !bound {
		return errors.New("apply_receipt_conflict")
	}
	if !matched {
		return errors.New("apply_reconciliation_failed")
	}
	return nil
}

func resolveOrderDependencies(
	ctx context.Context,
	tx pgx.Tx,
	p preparedOrders,
	event preparedOrderEvent,
) (map[string]string, error) {
	dependency := p.EventDependencies[event.Catalog.Catalog.EventID]
	var eventID string
	if err := tx.QueryRow(ctx, `SELECT r.event_id FROM core.legacy_event_references r JOIN core.pass_events e ON e.id=r.event_id
	WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND r.event_id=$3 FOR SHARE OF r,e`, dependency.Legacy.Key, dependency.Legacy.RecordSHA256, dependency.EventID).
		Scan(&eventID); err != nil {
		return nil, errors.New("apply_event_identity_unresolved")
	}
	owners := map[string]string{}
	for _, telegramID := range orderEventUsers(event) {
		user := p.Users[telegramID]
		var owner string
		err := tx.QueryRow(ctx, `SELECT r.owner FROM core.legacy_user_references r JOIN core.telegram_identities t ON t.owner=r.owner
		JOIN core.users u ON u.id=r.owner WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND t.bot_id=$3 AND t.telegram_id=$4
		AND u.telegram_id=$4 FOR SHARE OF r,t,u`, user.Legacy.Key, user.Legacy.RecordSHA256, p.Plan.BotID, telegramID).
			Scan(&owner)
		if err != nil {
			return nil, errors.New("apply_owner_identity_unresolved")
		}
		owners[strconv.FormatInt(telegramID, 10)] = owner
	}
	return owners, nil
}

func insertOrderEvent(
	ctx context.Context,
	tx pgx.Tx,
	p preparedOrders,
	event preparedOrderEvent,
	owners map[string]string,
) error {
	c := event.Catalog.Catalog
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.order_events(id,deadline,menu,extras,transfer_instructions,transfer_instructions_localized)
	VALUES($1,$2,$3,$4,$5,$6)`,
		c.EventID,
		c.Deadline,
		c.Menu,
		c.Extras,
		c.Instructions,
		c.Localized,
	); err != nil {
		return errors.New("apply_target_conflict")
	}
	if err := insertOrderReference(ctx, tx, p, event.Catalog, c.EventID, c.EventID); err != nil {
		return err
	}
	for _, admin := range c.Admins {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.order_admins(event_id,owner,country,region) VALUES($1,$2,$3,$4)`,
			c.EventID,
			owners[strconv.FormatInt(admin.TelegramID, 10)],
			admin.Country,
			admin.Region,
		); err != nil {
			return errors.New("apply_admin_conflict")
		}
	}
	for _, row := range event.Orders {
		if err := insertImportedOrder(ctx, tx, p, row, owners, c); err != nil {
			return err
		}
	}
	for _, row := range event.Slots {
		s := row.Slot
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.order_capacity_slots(event_id,service,seat,reservation_id,reservation_attempt_token,reservation_attempt_created_at,reserved_at)
		VALUES($1,$2,$3,$4,$5,$6,$7)`,
			s.EventID,
			s.Service,
			s.Seat,
			s.ReservationID,
			s.Attempt,
			s.AttemptAt,
			s.ReservedAt,
		); err != nil {
			return errors.New("apply_capacity_conflict")
		}
		if err := insertOrderReference(ctx, tx, p, row, c.EventID, orderSlotID(s)); err != nil {
			return err
		}
	}
	return nil
}

func insertOrderReference(
	ctx context.Context,
	tx pgx.Tx,
	p preparedOrders,
	row OrderPlanRecord,
	event, target string,
) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_order_import_references(source_key,bot_id,event_id,source_domain,source_record_sha256,target_id,source_record)
	VALUES($1,$2,$3,$4,$5,$6,$7)`,
		row.Legacy.Key,
		p.Plan.BotID,
		event,
		row.Source,
		row.Legacy.RecordSHA256,
		target,
		row.Record,
	); err != nil {
		return errors.New("apply_legacy_conflict")
	}
	return nil
}

func insertImportedOrder(
	ctx context.Context,
	tx pgx.Tx,
	p preparedOrders,
	row OrderPlanRecord,
	owners map[string]string,
	catalog *OrderCatalog,
) error {
	o := row.Order
	owner := owners[strconv.FormatInt(o.TelegramID, 10)]
	proofID := ""
	if proof, exists := p.Proofs[row.Legacy.Key]; exists {
		proofID = "legacy-proof:" + row.Legacy.Key
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.order_proofs(id,owner,filename,body,created_at) VALUES($1,$2,'legacy-receipt',$3,$4)`,
			proofID,
			owner,
			proof.Body,
			o.ProofReceived,
		); err != nil {
			return errors.New("apply_proof_conflict")
		}
		raw, err := json.Marshal(proof.Reference)
		if err != nil {
			return errors.New("apply_proof_conflict")
		}
		proofRow := OrderPlanRecord{
			Source: orderStateProof,
			Record: raw,
			Legacy: UserLegacyReference{
				Key:          identityKey("proof:"+row.Legacy.Key, "proof_file"),
				RecordSHA256: proof.Hash,
			},
		}
		if err = insertOrderReference(ctx, tx, p, proofRow, o.EventID, proofID); err != nil {
			return err
		}
	}
	admin := ""
	if adminID := effectiveOrderAdmin(o, catalog); adminID > 0 {
		admin = owners[strconv.FormatInt(adminID, 10)]
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.orders(id,event_id,owner,version,choice,state,attempt,attempt_at,proof_file,payment_admin,country,created_at,updated_at)
	VALUES($1,$2,$3,1,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		o.ID,
		o.EventID,
		owner,
		o.Choice,
		o.State,
		o.Attempt,
		o.AttemptAt,
		proofID,
		admin,
		o.Country,
		o.CreatedAt,
		o.UpdatedAt,
	); err != nil {
		return errors.New("apply_order_conflict")
	}
	return insertOrderReference(ctx, tx, p, row, o.EventID, o.ID)
}

// Whole-event comparison also detects newly added target orders/slots/admins.
// Bytea stays inside PostgreSQL; no proof bytes or target rows enter diagnostics.
const orderSnapshotSQL = `SELECT jsonb_build_object(
 'event',(SELECT to_jsonb(e) FROM core.order_events e WHERE e.id=$1),
 'admins',COALESCE((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.owner) FROM core.order_admins a WHERE a.event_id=$1),'[]'),
 'orders',COALESCE((SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM core.orders o WHERE o.event_id=$1),'[]'),
 'slots',COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.service,s.seat) FROM core.order_capacity_slots s WHERE s.event_id=$1),'[]'),
 'references',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.source_key) FROM core.legacy_order_import_references r WHERE r.event_id=$1),'[]'),
 'proofs',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM core.order_proofs p
 JOIN core.legacy_order_import_references r ON r.target_id=p.id AND r.source_domain='proof' WHERE r.event_id=$1),'[]'))`
