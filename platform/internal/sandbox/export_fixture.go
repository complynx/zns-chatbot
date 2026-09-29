package sandbox

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

type ExportFixture struct {
	OrderID      string
	Choice       *orders.Choice
	AdminEnabled *bool
	BatchTag     string
	BatchCount   int
}

func (fixture ExportFixture) validate() error {
	const maxBatchOrders = 10001
	const maxTagBytes = 40
	actions := 0
	if fixture.OrderID != "" || fixture.Choice != nil {
		if fixture.OrderID == "" || fixture.Choice == nil {
			return errors.New("order ID and saved choice are both required")
		}
		actions++
	}
	if fixture.AdminEnabled != nil {
		actions++
	}
	if fixture.BatchTag != "" || fixture.BatchCount != 0 {
		if fixture.BatchTag == "" || len(fixture.BatchTag) > maxTagBytes ||
			strings.Trim(fixture.BatchTag, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" ||
			fixture.BatchCount < 0 || fixture.BatchCount > maxBatchOrders {
			return errors.New("invalid export batch fixture")
		}
		actions++
	}
	if actions != 1 {
		return errors.New("exactly one export fixture action is required")
	}
	return nil
}

// ApplyExportFixture creates synthetic historical data for independent black-box QA.
// It is a schema-owner CLI operation, never a business API operation.
func ApplyExportFixture(ctx context.Context, db *pgxpool.Pool, fixture ExportFixture) error {
	if err := fixture.validate(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or a reported fixture error.
	var event string
	if err = tx.QueryRow(ctx, `SELECT id FROM core.order_events WHERE id=$1 FOR UPDATE`, fixtureEvent).
		Scan(&event); err != nil {
		return err
	}
	switch {
	case fixture.Choice != nil:
		err = fixture.savedChoice(ctx, tx)
	case fixture.AdminEnabled != nil:
		err = fixture.admin(ctx, tx)
	default:
		err = fixture.batch(ctx, tx)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (fixture ExportFixture) savedChoice(ctx context.Context, tx pgx.Tx) error {
	result, err := tx.Exec(ctx, `UPDATE core.orders SET choice=$3,version=version+1,updated_at=clock_timestamp()
	 WHERE event_id=$1 AND id=$2 AND state='unpaid'`, fixtureEvent, fixture.OrderID, fixture.Choice)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("fixture requires an existing unpaid order")
	}
	return nil
}

func (fixture ExportFixture) admin(ctx context.Context, tx pgx.Tx) error {
	if *fixture.AdminEnabled {
		_, err := tx.Exec(
			ctx,
			`INSERT INTO core.order_admins(event_id,owner,country) VALUES($1,'bob','be') ON CONFLICT DO NOTHING`,
			fixtureEvent,
		)
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM core.order_admins WHERE event_id=$1 AND owner='bob'`, fixtureEvent)
	return err
}

func (fixture ExportFixture) batch(ctx context.Context, tx pgx.Tx) error {
	event := "qa-export-" + fixture.BatchTag
	if fixture.BatchCount == 0 {
		return clearExportBatch(ctx, tx, event)
	}
	_, err := tx.Exec(ctx, `INSERT INTO core.order_events(id,deadline,menu,extras)
	 SELECT $1,deadline,menu,extras FROM core.order_events WHERE id=$2`, event, fixtureEvent)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.order_admins(event_id,owner,country) VALUES($1,'bob','be')`,
		event,
	); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.orders(id,event_id,owner,version,choice,state)
	 SELECT $1||':'||n,$1,'alice',1,'{"days":{},"customer":"Synthetic export size fixture","total":0}'::jsonb,'unpaid'
	 FROM generate_series(1,$2::int) n`, event, fixture.BatchCount)
	return err
}

func clearExportBatch(ctx context.Context, tx pgx.Tx, event string) error {
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM core.order_events WHERE id=$1 FOR UPDATE`, event).
		Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var changed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.orders o WHERE o.event_id=$1
	 AND (o.state<>'unpaid' OR EXISTS(SELECT 1 FROM core.order_audit a WHERE a.order_id=o.id)))`, event).Scan(&changed)
	if err != nil {
		return err
	}
	if changed {
		return errors.New("export fixture was changed through business actions; refusing cleanup")
	}
	for _, query := range []string{
		`DELETE FROM core.orders WHERE event_id=$1`,
		`DELETE FROM core.order_admins WHERE event_id=$1`,
		`DELETE FROM core.order_events WHERE id=$1`,
	} {
		if _, err = tx.Exec(ctx, query, event); err != nil {
			return err
		}
	}
	return nil
}
