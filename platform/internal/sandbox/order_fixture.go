package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const fixtureEvent = "sandbox-festival"
const maxFixtureAge = 365 * 24 * time.Hour

type OrderFixture struct {
	Deadline     time.Time
	OrderID      string
	Age          time.Duration
	AdminCountry string
	Capacity     *CapacityFixture
}

type CapacityFixture struct {
	Extra     string
	Remaining int
	Reset     bool
}

func (fixture OrderFixture) validate() error {
	if fixture.Age < 0 || fixture.Age > maxFixtureAge || (fixture.OrderID == "" && fixture.Age != 0) ||
		(fixture.AdminCountry != "" && fixture.AdminCountry != "be" && fixture.AdminCountry != "ru") {
		return errors.New("invalid order fixture")
	}
	if fixture.Deadline.IsZero() && fixture.OrderID == "" && fixture.AdminCountry == "" && fixture.Capacity == nil {
		return errors.New("empty order fixture")
	}
	if fixture.Capacity != nil {
		const maxRemainingSeats = 1000
		capacity := fixture.Capacity
		if orders.Extras()[capacity.Extra].Capacity <= 0 ||
			(!capacity.Reset && (capacity.Remaining < 1 || capacity.Remaining > maxRemainingSeats)) ||
			(capacity.Reset && capacity.Remaining != 0) {
			return errors.New("invalid capacity fixture")
		}
	}
	return nil
}

// ApplyOrderFixture is a CLI-only test control using the sandbox schema-owner role.
func ApplyOrderFixture(ctx context.Context, db *pgxpool.Pool, fixture OrderFixture) error {
	if err := fixture.validate(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or a reported fixture error.
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM core.order_events WHERE id=$1 FOR UPDATE`, fixtureEvent).
		Scan(&id); err != nil {
		return err
	}
	if !fixture.Deadline.IsZero() {
		if _, err = tx.Exec(
			ctx,
			`UPDATE core.order_events SET deadline=$2 WHERE id=$1`,
			fixtureEvent,
			fixture.Deadline,
		); err != nil {
			return err
		}
	}
	if fixture.OrderID != "" {
		result, updateError := tx.Exec(
			ctx,
			`UPDATE core.orders SET created_at=clock_timestamp()-($3*interval '1 second') WHERE event_id=$1 AND id=$2`,
			fixtureEvent,
			fixture.OrderID,
			int64(fixture.Age/time.Second),
		)
		if updateError != nil {
			return updateError
		}
		if result.RowsAffected() != 1 {
			return errors.New("fixture order not found")
		}
	}
	if fixture.AdminCountry != "" {
		if _, err = tx.Exec(
			ctx,
			`UPDATE core.order_admins SET country=$2 WHERE event_id=$1 AND owner='bob'`,
			fixtureEvent,
			fixture.AdminCountry,
		); err != nil {
			return err
		}
	}
	if err = fixture.applyCapacity(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (fixture OrderFixture) applyCapacity(ctx context.Context, tx pgx.Tx) error {
	if fixture.Capacity == nil {
		return nil
	}
	capacity := fixture.Capacity
	target := orders.Extras()[capacity.Extra].Capacity
	var reserved int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM core.orders
		 WHERE event_id=$1 AND state IN ('proof','paid') AND choice->'extras' ? $2`, fixtureEvent, capacity.Extra).
		Scan(&reserved); err != nil {
		return err
	}
	if capacity.Reset && reserved > target {
		return errors.New("capacity reset would remove reservations; cancel excess QA-owned proofs first")
	}
	if !capacity.Reset {
		target = reserved + capacity.Remaining
	}
	_, err := tx.Exec(ctx, `UPDATE core.order_events SET extras=jsonb_set(extras,$2,to_jsonb($3::int)) WHERE id=$1`,
		fixtureEvent, []string{capacity.Extra, "capacity"}, target)
	return err
}
