package orders

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// All capacity writes share the order event lock held by Execute. Bare claims
// deliberately have no order foreign key: an imported claim can be in flight.
func ensureCapacitySlots(ctx context.Context, tx pgx.Tx, e Event) error {
	var orphaned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_capacity_slots s
	WHERE s.event_id=$1 AND s.reservation_id IS NOT NULL
	AND s.seat >= COALESCE(($2::jsonb->s.service->>'capacity')::integer,0))`, e.ID, e.Extras).Scan(&orphaned); err != nil {
		return err
	}
	if orphaned {
		return problem(http.StatusConflict, "capacity_configuration_conflict")
	}
	for service, extra := range e.Extras {
		if extra.Capacity <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO core.order_capacity_slots(event_id,service,seat)
		SELECT $1,$2,generate_series(0,$3::integer-1) ON CONFLICT DO NOTHING`, e.ID, service, extra.Capacity); err != nil {
			return err
		}
	}
	// Python only cleans up missing orders for claims with payment tokens.
	// A bare claim has no expiry; reserved_at is not a lease deadline.
	_, err := tx.Exec(ctx, `UPDATE core.order_capacity_slots s SET reservation_id=NULL,
	reservation_attempt_token=NULL,reservation_attempt_created_at=NULL,reserved_at=NULL
	WHERE s.event_id=$1 AND s.reservation_attempt_token IS NOT NULL AND NOT EXISTS
	(SELECT 1 FROM core.orders o WHERE o.event_id=s.event_id AND o.id=s.reservation_id AND o.state<>'deleted')`, e.ID)
	return err
}

func capacityToken(o Order) string {
	if o.Attempt != "" {
		return o.Attempt
	}
	if o.ProofFile != "" {
		return "legacy-proof:" + o.ProofFile
	}
	return "legacy-order:" + o.ID
}

func capacityTime(o Order) time.Time {
	if o.AttemptAt != nil {
		return *o.AttemptAt
	}
	return o.CreatedAt
}

func releaseCapacity(ctx context.Context, tx pgx.Tx, before, after Order) error {
	// Legacy deletion releases selected services even for an unpaid bare claim.
	// Payment cancellation must still match the attempt that owned the claim.
	deleted := after.State == "deleted"
	if !deleted && (!before.reserves() || after.reserves() && before.Attempt == after.Attempt) {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE core.order_capacity_slots SET reservation_id=NULL,
	reservation_attempt_token=NULL,reservation_attempt_created_at=NULL,reserved_at=NULL
	WHERE event_id=$1 AND reservation_id=$2 AND ($5 OR reservation_attempt_token=$3)
	AND $4::jsonb ? service`, before.EventID, before.ID, capacityToken(before), before.Choice.Extras, deleted)
	return err
}

func capacityAvailable(ctx context.Context, tx pgx.Tx, event, service string, capacity int) (bool, error) {
	var available bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_capacity_slots
	WHERE event_id=$1 AND service=$2 AND seat<$3 AND reservation_id IS NULL)`, event, service, capacity).Scan(&available)
	return available, err
}

// Preserve an existing seat. A new payment may adopt only its own older attempt;
// otherwise use the first free seat, then displace strictly later paid priority.
func claimCapacity(ctx context.Context, tx pgx.Tx, o Order, service string, capacity int) (bool, error) {
	var seat int
	var attempt *string
	var created *time.Time
	token, at := capacityToken(o), capacityTime(o)
	err := tx.QueryRow(ctx, `SELECT seat,reservation_attempt_token,reservation_attempt_created_at
	FROM core.order_capacity_slots WHERE event_id=$1 AND service=$2 AND reservation_id=$3`, o.EventID, service, o.ID).
		Scan(&seat, &attempt, &created)
	if err == nil {
		if attempt != nil && *attempt == token {
			return true, nil
		}
		if created != nil && !at.After(*created) {
			return false, nil
		}
		return writeCapacityClaim(ctx, tx, o, service, seat)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRow(ctx, `SELECT seat FROM core.order_capacity_slots
	WHERE event_id=$1 AND service=$2 AND seat<$3 AND reservation_id IS NULL ORDER BY seat LIMIT 1`, o.EventID, service, capacity).Scan(&seat)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT seat FROM core.order_capacity_slots
		WHERE event_id=$1 AND service=$2 AND seat<$3 AND reservation_attempt_created_at IS NOT NULL
		AND (reservation_attempt_created_at,reservation_id)>($4,$5)
		ORDER BY reservation_attempt_created_at DESC,reservation_id DESC LIMIT 1`, o.EventID, service, capacity, at, o.ID).Scan(&seat)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return writeCapacityClaim(ctx, tx, o, service, seat)
}

func writeCapacityClaim(ctx context.Context, tx pgx.Tx, o Order, service string, seat int) (bool, error) {
	_, err := tx.Exec(ctx, `UPDATE core.order_capacity_slots SET reservation_id=$4,
	reservation_attempt_token=$5,reservation_attempt_created_at=$6,reserved_at=clock_timestamp()
	WHERE event_id=$1 AND service=$2 AND seat=$3`, o.EventID, service, seat, o.ID, capacityToken(o), capacityTime(o))
	return err == nil, err
}

func reconcileOrderCapacity(ctx context.Context, tx pgx.Tx, o *Order, e Event) ([]string, error) {
	removed := []string{}
	for service, extra := range e.Extras {
		price, selected := o.Choice.Extras[service]
		if !selected || extra.Capacity <= 0 {
			continue
		}
		var available bool
		var err error
		if o.reserves() {
			available, err = claimCapacity(ctx, tx, *o, service, extra.Capacity)
		} else {
			available, err = capacityAvailable(ctx, tx, e.ID, service, extra.Capacity)
		}
		if err != nil {
			return nil, err
		}
		if available {
			continue
		}
		delete(o.Choice.Extras, service)
		o.Choice.Extras["total"] = max(0, o.Choice.Extras["total"]-price)
		o.Choice.Total = max(0, o.Choice.Total-price)
		removed = append(removed, service)
	}
	return removed, nil
}
