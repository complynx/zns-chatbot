package legacyfood

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/jackc/pgx/v5"
)

type operation struct {
	notificationRegistrations []delivery.Registration
	service                   Service
	tx                        pgx.Tx
	actor                     string
	command                   Command
	event                     Event
	now                       time.Time
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// PreparedCommand retains domain authorization and an exact receipt in the caller transaction.
type PreparedCommand struct {
	op        operation
	key, hash string
	result    Order
	found     bool
}

func (s Service) Execute(ctx context.Context, actor string, command Command) (Order, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Order{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	prepared, err := s.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return Order{}, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return Order{}, err
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

// LockEvent establishes the same event-before-actor order for manual and derived commands.
func (s Service) LockEvent(ctx context.Context, tx pgx.Tx, event string) error {
	_, err := s.event(ctx, tx, event, true)
	return err
}

func (s Service) PrepareInTx(ctx context.Context, tx pgx.Tx, actor string, command Command) (PreparedCommand, error) {
	p := PreparedCommand{op: operation{service: s, tx: tx, actor: actor, command: command}}
	if command.Key == "" || len(command.Key) > 200 || command.EventID == "" || command.Version < 0 {
		return p, problem("food_invalid_command")
	}
	if err := p.op.authorize(ctx); err != nil {
		return p, err
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return p, err
	}
	p.hash, p.key = digest(raw), digest([]byte(command.Key))
	var previous string
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,result FROM core.food_operations WHERE event_id=$1 AND actor=$2 AND key_hash=$3`, command.EventID, actor, p.key).
		Scan(&previous, &saved)
	if err == nil {
		if err = decodeStoredJSON(saved, &p.result); err != nil {
			return p, err
		}
		if p.hash != previous {
			return p, problem("idempotency_conflict")
		}
		p.found = true
		return p, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	return p, core.DatabaseOperationError(err)
}

func (p PreparedCommand) Replay() (Order, bool) { return p.result, p.found }

func (p PreparedCommand) Apply(ctx context.Context) (Order, error) {
	if p.found {
		return p.result, nil
	}
	op := p.op
	if err := checkCatalogRevision(op.event, op.command.CatalogRevision); err != nil {
		return Order{}, err
	}
	result, err := op.load(ctx)
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
	if _, err = op.tx.Exec(
		ctx,
		`INSERT INTO core.food_operations(event_id,actor,key_hash,request_hash,result) VALUES($1,$2,$3,$4,$5)`,
		op.command.EventID,
		op.actor,
		p.key,
		p.hash,
		result,
	); err != nil {
		return result, core.DatabaseOperationError(err)
	}
	return result, delivery.RegisterBatch(ctx, op.tx, op.service.Delivery.BotID, op.notificationRegistrations)
}

func (op *operation) authorize(ctx context.Context) error {
	var err error
	op.event, err = op.service.event(ctx, op.tx, op.command.EventID, true)
	if err != nil {
		return err
	}
	var permitted bool
	err = op.tx.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1 FOR NO KEY UPDATE`, op.actor).
		Scan(&permitted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !permitted) {
		return forbidden()
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if op.command.Name == commandAccept || op.command.Name == commandReject {
		if err = adminPermission(ctx, op.tx, op.actor, op.event.ID, "review", true); err != nil {
			return err
		}
	}
	return core.DatabaseOperationError(op.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&op.now))
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
		return core.DatabaseOperationError(err)
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
	return core.DatabaseOperationError(err)
}
