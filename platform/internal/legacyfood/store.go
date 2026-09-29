package legacyfood

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const orderColumns = `id,event_id,owner,version,meals,meal_total,complete,activities,activity_total,
 COALESCE(payment_admin,''),created_at,last_updated`

func loadOrder(ctx context.Context, q queryer, event, id, owner string) (Order, error) {
	var order Order
	err := q.QueryRow(ctx, `SELECT `+orderColumns+` FROM core.food_orders WHERE event_id=$1
 AND (($2<>'' AND id=$2) OR ($2='' AND owner=$3))`, event, id, owner).
		Scan(&order.ID, &order.EventID, &order.Owner, &order.Version, &order.Meals, &order.MealTotal, &order.Complete,
			&order.Activities, &order.ActivityTotal, &order.PaymentAdmin, &order.CreatedAt, &order.LastUpdated)
	if err != nil {
		return order, err
	}
	order.MealPayment, err = loadPayment(ctx, q, order.ID, Meals)
	if err != nil {
		return order, err
	}
	order.ActivityPayment, err = loadPayment(ctx, q, order.ID, Activity)
	return order, err
}

func loadPayment(ctx context.Context, q queryer, id, kind string) (Payment, error) {
	payment := Payment{Kind: kind, Status: Pending}
	err := q.QueryRow(ctx, `SELECT generation,status,COALESCE(proof_id,''),proof_source,COALESCE(receiver,''),received_at,
 COALESCE(confirmed_by,''),confirmed_at,COALESCE(rejected_by,''),rejected_at,COALESCE(legacy_source_key,'')
 FROM core.food_payments WHERE order_id=$1 AND kind=$2 ORDER BY generation DESC LIMIT 1`, id, kind).
		Scan(&payment.Generation, &payment.Status, &payment.ProofID, &payment.ProofSource, &payment.Receiver,
			&payment.ReceivedAt, &payment.ConfirmedBy, &payment.ConfirmedAt, &payment.RejectedBy, &payment.RejectedAt, &payment.LegacySourceKey)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return payment, err
}

func (s Service) event(ctx context.Context, q queryer, id string, lock bool) (Event, error) {
	var event Event
	query := `SELECT f.event_id,f.menu,f.menu_sha256,f.meal_prices,f.activity_prices,f.deadline,f.cacao_capacity,
 p.finishes_at>clock_timestamp() FROM core.food_events f JOIN core.pass_events p ON p.id=f.event_id
 WHERE f.event_id=$1 AND f.bot_id=$2`
	if lock {
		query += ` FOR UPDATE OF f`
	}
	err := q.QueryRow(ctx, query, id, s.BotID).Scan(&event.ID, &event.Menu, &event.MenuSHA256, &event.MealPrices,
		&event.ActivityPrices, &event.Deadline, &event.CacaoCapacity, &event.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		err = problem("food_event_unavailable")
	}
	return event, err
}

func allowed(ctx context.Context, q queryer, actor string) error {
	var permitted bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1 AND can_book)`, actor).Scan(&permitted)
	if err != nil {
		return err
	}
	if !permitted {
		return forbidden()
	}
	return nil
}

func adminAllowed(ctx context.Context, q queryer, actor, event, scope string) error {
	return adminPermission(ctx, q, actor, event, scope, false)
}

func adminPermission(ctx context.Context, q queryer, actor, event, scope string, lock bool) error {
	query := `SELECT CASE $3 WHEN 'export' THEN can_export WHEN 'review' THEN can_review ELSE false END
 FROM core.food_admins WHERE event_id=$1 AND owner=$2`
	if lock {
		query += " FOR SHARE"
	}
	var permitted bool
	err := q.QueryRow(ctx, query, event, actor, scope).Scan(&permitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	if err != nil {
		return err
	}
	if !permitted {
		return forbidden()
	}
	return nil
}

func (s Service) Event(ctx context.Context, actor, event string) (Event, error) {
	if err := allowed(ctx, s.DB, actor); err != nil {
		return Event{}, err
	}
	return s.event(ctx, s.DB, event, false)
}

func (s Service) CurrentEvent(ctx context.Context, actor string) (Event, error) {
	if err := allowed(ctx, s.DB, actor); err != nil {
		return Event{}, err
	}
	var id string
	err := s.DB.QueryRow(ctx, `SELECT f.event_id FROM core.food_events f JOIN core.pass_events p ON p.id=f.event_id
 WHERE f.bot_id=$1 AND p.finishes_at>clock_timestamp() ORDER BY p.display_order,p.finishes_at,p.id LIMIT 1`, s.BotID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, problem("food_event_unavailable")
	}
	if err != nil {
		return Event{}, err
	}
	return s.event(ctx, s.DB, id, false)
}

func (s Service) Get(ctx context.Context, actor, event, id string) (Order, error) {
	if _, err := s.Event(ctx, actor, event); err != nil {
		return Order{}, err
	}
	order, err := loadOrder(ctx, s.DB, event, id, actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, problem("food_order_not_found")
	}
	if err != nil {
		return order, err
	}
	if order.Owner != actor {
		if err = adminAllowed(ctx, s.DB, actor, event, "review"); err != nil {
			return Order{}, err
		}
	}
	return order, nil
}
