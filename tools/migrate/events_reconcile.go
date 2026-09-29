package migrate

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func reconcileEvent(ctx context.Context, tx pgx.Tx, plan preparedEvents, row EventPlanRecord) error {
	event := row.Candidate
	titles, err := json.Marshal(event.Titles)
	if err != nil {
		return errors.New("event_title_invalid")
	}
	var matched bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM core.pass_events e JOIN core.legacy_event_references l ON l.event_id=e.id
WHERE e.id=$1 AND e.finishes_at=$2 AND e.passport_required=$3 AND e.assignment_rule=$4 AND e.disable_concurrency_limit=$5 AND e.titles=$6::jsonb AND e.display_order=$7 AND l.source_key=$8 AND l.source_record_sha256=$9
AND (SELECT count(*) FROM core.pass_event_tiers WHERE event_id=e.id)=$10
AND (SELECT count(*) FROM core.pass_payment_admins WHERE event_id=e.id)=$11)`, event.ID, event.FinishesAt, event.PassportRequired, event.AssignmentRule, event.DisableConcurrencyLimit, titles, plan.orders[row.Legacy.Key], row.Legacy.Key, row.Legacy.RecordSHA256, len(event.Tiers), len(event.Admins)).
		Scan(&matched)
	if err != nil || !matched {
		return errors.New("apply_reconciliation_failed")
	}
	if err = reconcileEventConfiguration(ctx, tx, event); err != nil {
		return err
	}
	if err = reconcileEventTiers(ctx, tx, event); err != nil {
		return err
	}
	return reconcileEventAdmins(ctx, tx, plan.plan.BotID, event)
}

func reconcileEventTiers(ctx context.Context, tx pgx.Tx, event *EventCandidate) error {
	for position, tier := range event.Tiers {
		var matched bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM core.pass_event_tiers WHERE event_id=$1 AND position=$2 AND amount=$3 AND price=$4 AND starts_at=$5 AND promo=$6 AND blocked_by_date=$7)`, event.ID, position, tier.Amount, tier.Price, tier.StartsAt, tier.Promo, tier.BlockedByDate).
			Scan(&matched)
		if err != nil || !matched {
			return errors.New("apply_reconciliation_failed")
		}
	}
	return nil
}

func reconcileEventAdmins(ctx context.Context, tx pgx.Tx, botID int64, event *EventCandidate) error {
	for _, admin := range event.Admins {
		var matched bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM core.pass_payment_admins a JOIN core.telegram_identities t ON t.owner=a.owner JOIN core.users u ON u.id=t.owner WHERE a.event_id=$1 AND a.hidden=$2 AND t.bot_id=$3 AND t.telegram_id=$4 AND u.telegram_id=$4)`, event.ID, admin.Hidden, botID, admin.TelegramID).
			Scan(&matched)
		if err != nil || !matched {
			return errors.New("apply_reconciliation_failed")
		}
	}
	return nil
}
