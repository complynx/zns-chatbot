package credits

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// OperatorChange is never exposed as an agent tool. Every correction requires
// current administrator authority, a durable replay key and external evidence.
type OperatorChange struct {
	Key       string         `json:"key"`
	Kind      string         `json:"kind"`
	Evidence  string         `json:"evidence"`
	Revision  *PriceRevision `json:"revision,omitempty"`
	Provider  string         `json:"provider,omitempty"`
	Model     string         `json:"model,omitempty"`
	Version   string         `json:"version,omitempty"`
	AttemptID string         `json:"attempt_id,omitempty"`
	Payer     string         `json:"payer,omitempty"`
	Period    time.Time      `json:"period,omitzero"`
	Amount    *int64         `json:"amount_nano_usd,omitempty"`
}

func (s Service) Operate(ctx context.Context, actor string, change OperatorChange) error {
	if change.Kind == "cutover" && !s.Enforce {
		return ErrInvalid
	}
	if actor == "" || len(actor) > 256 || change.Key == "" || len(change.Key) > 128 || change.Evidence == "" ||
		len(change.Evidence) > 2048 {
		return ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireAdmin(ctx, tx, actor); err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,65001))`,
		actor+":"+change.Key,
	); err != nil {
		return err
	}
	raw, err := json.Marshal(change)
	if err != nil {
		return ErrInvalid
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT request=$3::jsonb FROM credits.operator_changes WHERE actor=$1 AND operation_key=$2`, actor, change.Key, raw).
		Scan(&same)
	if err == nil {
		if !same {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO credits.operator_changes(actor,operation_key,request) VALUES($1,$2,$3)`,
		actor,
		change.Key,
		raw,
	); err != nil {
		return err
	}
	if err = applyOperator(ctx, tx, actor, change); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func applyOperator(ctx context.Context, tx pgx.Tx, actor string, change OperatorChange) error {
	switch change.Kind {
	case "register_price":
		if change.Revision == nil {
			return ErrInvalid
		}
		return registerPrice(ctx, tx, *change.Revision)
	case "select_price":
		return selectPrice(ctx, tx, change.Provider, change.Model, change.Version)
	case "reconcile":
		return reconcile(ctx, tx, actor, change)
	case "release_unsent":
		return releaseReserved(ctx, tx, change.AttemptID)
	case "adjust":
		if change.Amount == nil || change.Payer == "" || len(change.Payer) > 256 ||
			!change.Period.Equal(month(change.Period)) ||
			change.Period.IsZero() {
			return ErrInvalid
		}
		if _, err := lockPolicy(ctx, tx, change.Payer); err != nil {
			return err
		}
		_, err := tx.Exec(
			ctx,
			`INSERT INTO credits.adjustments(actor,operation_key,payer,period_start,delta_nano_usd,evidence) VALUES($1,$2,$3,$4,$5,$6)`,
			actor,
			change.Key,
			change.Payer,
			change.Period,
			*change.Amount,
			change.Evidence,
		)
		return err
	case "cutover":
		// Activation is explicit and immutable. Existing reservations keep their mode.
		_, err := tx.Exec(
			ctx,
			`INSERT INTO credits.cutover(singleton,epoch,actor,operation_key) VALUES(true,clock_timestamp(),$1,$2)`,
			actor,
			change.Key,
		)
		return err
	default:
		return ErrInvalid
	}
}

func releaseReserved(ctx context.Context, tx pgx.Tx, id string) error {
	var payer string
	if err := tx.QueryRow(ctx, `SELECT payer FROM credits.attempts WHERE id::text=$1`, id).Scan(&payer); err != nil {
		return err
	}
	if _, err := lockPolicy(ctx, tx, payer); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE credits.attempts SET state='not_sent' WHERE id::text=$1 AND state='reserved'`, id)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

func reconcile(ctx context.Context, tx pgx.Tx, actor string, change OperatorChange) error {
	if change.Amount == nil || *change.Amount < 0 || change.AttemptID == "" {
		return ErrInvalid
	}
	var payer string
	if err := tx.QueryRow(ctx, `SELECT payer FROM credits.attempts WHERE id::text=$1`, change.AttemptID).
		Scan(&payer); err != nil {
		return err
	}
	if _, err := lockPolicy(ctx, tx, payer); err != nil {
		return err
	}
	var state string
	var cost *int64
	if err := tx.QueryRow(ctx, `SELECT state,cost_nano_usd FROM credits.attempts WHERE id::text=$1 FOR UPDATE`, change.AttemptID).
		Scan(&state, &cost); err != nil {
		return err
	}
	if (state != "dispatched" && state != "settled") || cost != nil {
		return ErrConflict
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO credits.reconciliations(attempt_id,cost_nano_usd,evidence,actor,operation_key) VALUES($1,$2,$3,$4,$5)`,
		change.AttemptID,
		*change.Amount,
		change.Evidence,
		actor,
		change.Key,
	)
	return err
}
