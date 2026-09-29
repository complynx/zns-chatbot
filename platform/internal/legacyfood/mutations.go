package legacyfood

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
)

func (op *operation) apply(ctx context.Context, order *Order) error {
	switch op.command.Name {
	case commandSaveMeals, commandDeleteMeals:
		return op.saveMeals(order)
	case commandToggleActivity:
		return op.toggle(ctx, order)
	case "begin_payment":
		return op.beginPayment(ctx, order)
	case "prepare_activities":
		return op.assignReceiver(ctx, order)
	case "submit_proof":
		return op.submitProof(ctx, order)
	case commandAccept, commandReject:
		return op.review(ctx, order)
	default:
		return problem("food_invalid_action")
	}
}

func (op *operation) saveMeals(order *Order) error {
	if !op.event.Active {
		return problem("food_event_inactive")
	}
	selection := op.command.Meals
	if selection == nil || op.command.Name == commandDeleteMeals {
		selection = MealSelection{}
	}
	var menu Menu
	if err := json.Unmarshal(op.event.Menu, &menu); err != nil {
		return err
	}
	quote, err := QuoteMeals(menu, op.event.MealPrices, selection)
	if err != nil {
		return problem(err.Error())
	}
	if order.MealPayment.Locked() {
		if !reflect.DeepEqual(selection, order.Meals) || quote.Total != order.MealTotal {
			return problem("food_payment_locked")
		}
	} else {
		if order.MealPayment.Status != Pending {
			order.MealPayment = Payment{Kind: Meals, Generation: order.MealPayment.Generation + 1, Status: Pending}
		}
		order.PaymentAdmin = ""
	}
	order.Meals, order.MealTotal, order.Complete = selection, quote.Total, quote.Complete
	order.LastUpdated = &op.now
	return nil
}

func (op *operation) toggle(ctx context.Context, order *Order) error {
	if !op.event.Active {
		return problem("food_event_inactive")
	}
	if order.ActivityPayment.Locked() {
		return problem("food_payment_locked")
	}
	var selected int
	err := op.tx.QueryRow(ctx, `SELECT count(*) FROM core.food_orders WHERE event_id=$1 AND activities->'cacao'='true'::jsonb`, order.EventID).
		Scan(&selected)
	if err != nil {
		return err
	}
	order.Activities, err = ToggleActivities(order.Activities, op.command.Activity, selected < op.event.CacaoCapacity)
	if err != nil {
		return problem(err.Error())
	}
	order.ActivityTotal, err = QuoteActivities(op.event.ActivityPrices, order.Activities)
	return err
}

func payment(order *Order, kind string) (*Payment, error) {
	switch kind {
	case Meals:
		return &order.MealPayment, nil
	case Activity:
		return &order.ActivityPayment, nil
	default:
		return nil, problem("food_invalid_payment_kind")
	}
}

func (op *operation) beginPayment(ctx context.Context, order *Order) error {
	p, err := payment(order, op.command.Kind)
	if err != nil {
		return err
	}
	if p.Locked() {
		return problem("food_payment_locked")
	}
	if op.command.Kind == Meals && (!order.Complete || op.now.After(op.event.Deadline)) {
		return problem("food_payment_unavailable")
	}
	if op.command.Kind == Activity && order.ActivityTotal <= 0 {
		return problem("food_payment_unavailable")
	}
	return op.assignReceiver(ctx, order)
}

func (op *operation) assignReceiver(ctx context.Context, order *Order) error {
	if !op.event.Active {
		return problem("food_event_inactive")
	}
	if order.PaymentAdmin != "" {
		return op.receiverAvailable(ctx, order)
	}
	err := op.tx.QueryRow(ctx, `SELECT a.owner FROM core.food_admins a JOIN core.users u ON u.id=a.owner
 WHERE a.event_id=$1 AND a.can_assign AND u.can_book ORDER BY random() LIMIT 1`, order.EventID).Scan(&order.PaymentAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem("food_payment_admin_unavailable")
	}
	return err
}

func (op *operation) submitProof(ctx context.Context, order *Order) error {
	p, err := payment(order, op.command.Kind)
	if err != nil {
		return err
	}
	if p.Locked() || op.command.Generation != p.Generation {
		return problem("food_stale_payment")
	}
	if err = op.receiverAvailable(ctx, order); err != nil {
		return err
	}
	var owned bool
	err = op.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_proofs WHERE id=$1 AND owner=$2)`, op.command.ProofID, order.Owner).
		Scan(&owned)
	if err != nil {
		return err
	}
	if !owned {
		return problem("food_invalid_proof")
	}
	*p = Payment{Kind: p.Kind, Generation: p.Generation + 1, Status: Submitted, ProofID: op.command.ProofID,
		Receiver: order.PaymentAdmin, ReceivedAt: &op.now}
	return op.notice(ctx, order, p.Receiver, "proof_submitted", *p)
}

func (op *operation) receiverAvailable(ctx context.Context, order *Order) error {
	var exists bool
	err := op.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.food_admins a JOIN core.users u ON u.id=a.owner
 WHERE a.event_id=$1 AND a.owner=$2 AND (a.can_assign OR a.can_review) AND u.can_book)`, order.EventID, order.PaymentAdmin).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return problem("food_payment_admin_unavailable")
	}
	return nil
}

func (op *operation) review(ctx context.Context, order *Order) error {
	p, err := payment(order, op.command.Kind)
	if err != nil {
		return err
	}
	if p.Status != Submitted || p.Generation != op.command.Generation {
		return problem("food_stale_payment")
	}
	if op.command.Name == commandAccept {
		p.Status, p.ConfirmedBy, p.ConfirmedAt = Paid, op.actor, &op.now
	} else {
		p.Status, p.RejectedBy, p.RejectedAt = Rejected, op.actor, &op.now
	}
	return op.notice(ctx, order, order.Owner, p.Status, *p)
}

func (op *operation) notice(ctx context.Context, order *Order, owner, kind string, p Payment) error {
	_, err := op.tx.Exec(
		ctx,
		`INSERT INTO core.food_notifications(event_id,owner,kind,subject,payload)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		order.EventID,
		owner,
		kind,
		fmt.Sprintf(
			"%s:%s:%d",
			order.ID,
			p.Kind,
			p.Generation,
		),
		map[string]any{"order_id": order.ID, "kind": p.Kind, "generation": p.Generation},
	)
	return err
}
