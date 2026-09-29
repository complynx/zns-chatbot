package legacyfood

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type operation struct {
	service Service
	tx      pgx.Tx
	actor   string
	command Command
	event   Event
	now     time.Time
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func (s Service) Execute(ctx context.Context, actor string, command Command) (Order, error) {
	if command.Key == "" || len(command.Key) > 200 || command.EventID == "" || command.Version < 0 {
		return Order{}, problem("food_invalid_command")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	op := operation{service: s, tx: tx, actor: actor, command: command}
	if err = op.authorize(ctx); err != nil {
		return Order{}, err
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return Order{}, err
	}
	hash, key := digest(raw), digest([]byte(command.Key))
	var previous string
	var result Order
	err = tx.QueryRow(ctx, `SELECT request_hash,result FROM core.food_operations WHERE event_id=$1 AND actor=$2 AND key_hash=$3`,
		command.EventID, actor, key).
		Scan(&previous, &result)
	if err == nil {
		if hash != previous {
			return Order{}, problem("idempotency_conflict")
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Order{}, err
	}
	// authorize holds the event row lock until commit. A new command cannot
	// reinterpret observed indices/prices; an exact completed replay stays exact.
	if err = checkCatalogRevision(op.event, command.CatalogRevision); err != nil {
		return Order{}, err
	}
	result, err = op.load(ctx)
	if err != nil {
		return Order{}, err
	}
	if err = op.apply(ctx, &result); err != nil {
		return Order{}, err
	}
	result.Version++
	if err = op.save(ctx, result); err != nil {
		return Order{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.food_operations(event_id,actor,key_hash,request_hash,result) VALUES($1,$2,$3,$4,$5)`,
		command.EventID,
		actor,
		key,
		hash,
		result,
	)
	if err != nil {
		return Order{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return result, nil
}

func (op *operation) authorize(ctx context.Context) error {
	var permitted bool
	err := op.tx.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1 FOR NO KEY UPDATE`, op.actor).
		Scan(&permitted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !permitted) {
		return forbidden()
	}
	if err != nil {
		return err
	}
	op.event, err = op.service.event(ctx, op.tx, op.command.EventID, true)
	if err != nil {
		return err
	}
	if op.command.Name == commandAccept || op.command.Name == commandReject {
		if err = adminAllowed(ctx, op.tx, op.actor, op.event.ID, "review"); err != nil {
			return err
		}
	}
	return op.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&op.now)
}

func (op *operation) load(ctx context.Context) (Order, error) {
	order, err := loadOrder(ctx, op.tx, op.event.ID, op.command.OrderID, op.actor)
	if errors.Is(err, pgx.ErrNoRows) && op.command.OrderID == "" && op.command.Version == 0 &&
		(op.command.Name == commandSaveMeals || op.command.Name == commandToggleActivity) {
		return Order{
			ID:              "food:" + rand.Text(),
			EventID:         op.event.ID,
			Owner:           op.actor,
			Meals:           MealSelection{},
			Activities:      Activities{},
			CreatedAt:       op.now,
			MealPayment:     Payment{Kind: Meals, Status: Pending},
			ActivityPayment: Payment{Kind: Activity, Status: Pending},
		}, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, problem("food_order_not_found")
	}
	if err != nil {
		return order, err
	}
	if op.command.Name != commandAccept && op.command.Name != commandReject && order.Owner != op.actor {
		return Order{}, forbidden()
	}
	if order.Version != op.command.Version {
		return Order{}, problem("stale_version")
	}
	return order, nil
}

func (op *operation) save(ctx context.Context, order Order) error {
	_, err := op.tx.Exec(
		ctx,
		`INSERT INTO core.food_orders(id,event_id,owner,version,meals,meal_total,complete,activities,
 activity_total,payment_admin,created_at,last_updated) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12)
 ON CONFLICT(id) DO UPDATE SET version=EXCLUDED.version,meals=EXCLUDED.meals,meal_total=EXCLUDED.meal_total,
 complete=EXCLUDED.complete,activities=EXCLUDED.activities,activity_total=EXCLUDED.activity_total,
 payment_admin=EXCLUDED.payment_admin,last_updated=EXCLUDED.last_updated`,
		order.ID,
		order.EventID,
		order.Owner,
		order.Version,
		order.Meals,
		order.MealTotal,
		order.Complete,
		order.Activities,
		order.ActivityTotal,
		order.PaymentAdmin,
		order.CreatedAt,
		order.LastUpdated,
	)
	if err != nil {
		return err
	}
	for _, payment := range []Payment{order.MealPayment, order.ActivityPayment} {
		if err = op.savePayment(ctx, order.ID, payment); err != nil {
			return err
		}
	}
	return nil
}

func (op *operation) savePayment(ctx context.Context, id string, p Payment) error {
	_, err := op.tx.Exec(
		ctx,
		`INSERT INTO core.food_payments(order_id,kind,generation,status,proof_id,proof_source,receiver,
 received_at,confirmed_by,confirmed_at,rejected_by,rejected_at,legacy_source_key)
 VALUES($1,$2,$3,$4,NULLIF($5,''),$6,NULLIF($7,''),$8,NULLIF($9,''),$10,NULLIF($11,''),$12,NULLIF($13,''))
 ON CONFLICT(order_id,kind,generation) DO UPDATE SET status=EXCLUDED.status,
 confirmed_by=EXCLUDED.confirmed_by,confirmed_at=EXCLUDED.confirmed_at,
 rejected_by=EXCLUDED.rejected_by,rejected_at=EXCLUDED.rejected_at`,
		id,
		p.Kind,
		p.Generation,
		p.Status,
		p.ProofID,
		p.ProofSource,
		p.Receiver,
		p.ReceivedAt,
		p.ConfirmedBy,
		p.ConfirmedAt,
		p.RejectedBy,
		p.RejectedAt,
		p.LegacySourceKey,
	)
	return err
}
