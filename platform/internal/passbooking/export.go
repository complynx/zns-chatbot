package passbooking

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const MaxExportBytes = 20 << 20
const maxExportInputBytes = 16 << 20
const maxExportRows = 10000

const exportEvents = `WITH allowed_events AS (
 SELECT e.id FROM core.pass_events e JOIN core.users actor ON actor.id=$1 AND actor.can_book
 WHERE e.finishes_at>transaction_timestamp() AND
 (EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=actor.id)
 OR EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.owner=actor.id AND a.event_id=e.id))) `

// Source column order is retained; public Telegram metadata is distinct from legal identity.
const exportProjection = `SELECT jsonb_build_array(u.telegram_id::text,u.username,u.first_name,u.last_name,u.print_name,COALESCE(profile.legal_name,''),u.language,
 b.event_id,b.state,b.kind,b.role,COALESCE(partner_user.telegram_id::text,''),
 b.price::bigint+COALESCE(partner.price,0),b.price,CASE WHEN legacy_assignment.source_key IS NOT NULL THEN legacy_assignment.assignment_tier_number::bigint ELSE b.tier_index::bigint+1 END,
 to_char(b.created_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.MS'),
 to_char(b.assigned_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.MS'),
 admin.telegram_id::text,COALESCE(receiver.telegram_id::text,legacy_receiver.telegram_id::text,CASE WHEN b.state='paid' AND NOT EXISTS(SELECT 1 FROM core.legacy_pass_import_references source WHERE source.event_id=b.event_id AND source.owner=b.owner AND source.source_kind IN ('booking','embedded')) THEN admin.telegram_id::text END),
 to_char(COALESCE(payment.received_at,legacy_payment.received_at) AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.MS'),
 to_char(CASE WHEN payment.decision='accepted' THEN payment.reviewed_at WHEN payment.id IS NULL THEN legacy_payment.accepted_at END AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.MS'),
 to_char(GREATEST(rejected.reviewed_at,legacy_payment.rejected_at) AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.MS'),
 CASE WHEN payment.kind='free' THEN 'free_pass' WHEN payment.id IS NULL OR payment.legacy_source_key IS NOT NULL THEN NULLIF(legacy_payment.proof_reference,'') ELSE payment.proof_id END,b.skip_balance,b.comment) AS cells
 FROM core.pass_bookings b JOIN allowed_events e ON e.id=b.event_id JOIN core.users u ON u.id=b.owner
 LEFT JOIN core.pass_profiles profile ON profile.owner=b.owner
 LEFT JOIN core.pass_bookings partner ON partner.event_id=b.event_id AND partner.owner=b.partner
 LEFT JOIN core.users partner_user ON partner_user.id=b.partner
 LEFT JOIN core.users admin ON admin.id=b.payment_admin
 LEFT JOIN core.pass_payment_attempts payment ON payment.id=b.payment_attempt AND payment.event_id=b.event_id
 LEFT JOIN core.legacy_pass_assignment_metadata legacy_assignment ON legacy_assignment.event_id=b.event_id AND legacy_assignment.owner=b.owner AND legacy_assignment.assigned_at=b.assigned_at
 LEFT JOIN core.legacy_pass_payment_metadata legacy_payment ON legacy_payment.event_id=b.event_id AND legacy_payment.owner=b.owner AND legacy_payment.assigned_at=b.assigned_at
 LEFT JOIN core.users receiver ON receiver.id=CASE WHEN payment.legacy_source_key IS NOT NULL THEN COALESCE(legacy_payment.receiving_admin,payment.receiving_admin) ELSE payment.receiving_admin END
 LEFT JOIN core.pass_receiver_backfills backfill ON backfill.event_id=b.event_id AND backfill.owner=b.owner AND backfill.assigned_at=b.assigned_at
 LEFT JOIN core.users legacy_receiver ON legacy_receiver.id=backfill.legacy_receiving_admin
 LEFT JOIN LATERAL (
 SELECT max(attempt.reviewed_at) AS reviewed_at FROM core.pass_payment_attempts attempt
 JOIN core.pass_payment_participants participant ON participant.attempt=attempt.id
 WHERE attempt.event_id=b.event_id AND participant.owner=b.owner
 AND participant.assigned_at=b.assigned_at AND attempt.decision='rejected'
 ) rejected ON true
 WHERE b.state<>'cancelled'`

// ExportSnapshot returns a consistent file and the complete event authority set.
func (s Service) ExportSnapshot(ctx context.Context, actor string) (ExportSnapshot, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ExportSnapshot{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var allowed bool
	if err = tx.QueryRow(ctx, exportEvents+`SELECT EXISTS(SELECT 1 FROM allowed_events)`, actor).
		Scan(&allowed); err != nil {
		return ExportSnapshot{}, core.DatabaseOperationError(err)
	}
	if !allowed {
		return ExportSnapshot{}, forbidden()
	}
	events, err := exportSnapshotEvents(ctx, tx, actor)
	if err != nil {
		return ExportSnapshot{}, err
	}
	var count, size int64
	err = tx.QueryRow(ctx, exportEvents+`, export_rows AS (`+exportProjection+`)
 SELECT count(*),COALESCE(sum(octet_length(cells::text)),0) FROM export_rows`, actor).Scan(&count, &size)
	if err != nil {
		return ExportSnapshot{}, core.DatabaseOperationError(err)
	}
	if count > maxExportRows || size > maxExportInputBytes {
		return ExportSnapshot{}, exportTooLarge()
	}
	rows, err := tx.Query(ctx, exportEvents+exportProjection+` ORDER BY b.event_id,b.created_at,u.telegram_id`, actor)
	if err != nil {
		return ExportSnapshot{}, core.DatabaseOperationError(err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]any, error) {
		var cells []any
		scanErr := row.Scan(&cells)
		return cells, scanErr
	})
	if err != nil {
		return ExportSnapshot{}, core.DatabaseOperationError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return ExportSnapshot{}, core.DatabaseOperationError(err)
	}
	body, err := renderPassExport(ctx, list)
	return ExportSnapshot{Body: body, Events: events}, err
}

func exportTooLarge() error {
	return &core.ProblemError{Status: http.StatusRequestEntityTooLarge, Code: "export_too_large"}
}
