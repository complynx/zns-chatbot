package migrate

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Food uses event timing and catalog order; preserve the full imported catalog
// row, which contains configuration only, while leaving booking state independent.
func resolveFoodEventDependency(ctx context.Context, tx pgx.Tx, p preparedFood, event preparedFoodEvent) error {
	dep := p.Dependencies[event.Catalog.Configuration.Event]
	var receipt map[string]any
	var matched bool
	// Only the event importer can establish either anchor after full reconciliation.
	// to_jsonb also handles old importer receipts without the snapshot columns.
	err := tx.QueryRow(ctx, `SELECT to_jsonb(c),COALESCE(c.plan_sha256=$4 AND to_jsonb(c)->'event_snapshot'=to_jsonb(e)
 AND to_jsonb(c)->'receipt_snapshot'=jsonb_build_object('source_key',c.source_key,'plan_sha256',c.plan_sha256,'resolution_sha256',c.resolution_sha256,'admin_sha256',c.admin_sha256),false)
 FROM core.legacy_event_references r JOIN core.pass_events e ON e.id=r.event_id
 JOIN migrate_import.event_receipts c ON c.source_key=r.source_key
 WHERE r.source_key=$1 AND r.source_record_sha256=$2 AND r.event_id=$3 FOR SHARE OF r,e,c`, dep.Legacy.Key, dep.Legacy.RecordSHA256, dep.EventID, p.EventPlanSHA256).
		Scan(&receipt, &matched)
	if receipt != nil && (receipt["event_snapshot"] == nil || receipt["receipt_snapshot"] == nil) {
		return errors.New("food_event_receipt_reconcile_required")
	}
	if err != nil || !matched {
		return errors.New("food_event_dependency_changed")
	}
	return nil
}
