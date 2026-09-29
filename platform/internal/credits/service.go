package credits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB      *pgxpool.Pool
	Enforce bool
}

func (s Service) RequestTier() string {
	if s.Enforce {
		return "default"
	}
	return ""
}

func (s Service) Reserve(ctx context.Context, a Attempt) error {
	if _, err := uuid.Parse(a.ID); err != nil {
		return ErrInvalid
	}
	for _, value := range []string{a.Scope.Actor, a.Scope.Payer, a.Scope.Key, a.Operation, a.Provider, a.Model} {
		if value == "" || len(value) > 256 {
			return ErrInvalid
		}
	}
	if a.ReservedNanoUSD != nil && *a.ReservedNanoUSD < 0 {
		return ErrInvalid
	}
	if s.Enforce && !a.Scope.LegacyBudget {
		return s.reserveEnforced(ctx, a)
	}
	tag, err := s.DB.Exec(
		ctx,
		`INSERT INTO credits.attempts(id,operation_key,actor,payer,operation,provider,model,state,reserved_nano_usd,price_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,'reserved',$8,(SELECT version FROM credits.price_selection WHERE provider=$6 AND model=$7)) ON CONFLICT(id) DO UPDATE SET id=EXCLUDED.id
 WHERE credits.attempts.operation_key=EXCLUDED.operation_key AND credits.attempts.actor=EXCLUDED.actor
 AND credits.attempts.payer=EXCLUDED.payer AND credits.attempts.operation=EXCLUDED.operation
 AND credits.attempts.provider=EXCLUDED.provider AND credits.attempts.model=EXCLUDED.model
 AND credits.attempts.reserved_nano_usd IS NOT DISTINCT FROM EXCLUDED.reserved_nano_usd`,
		a.ID,
		a.Scope.Key,
		a.Scope.Actor,
		a.Scope.Payer,
		a.Operation,
		a.Provider,
		a.Model,
		a.ReservedNanoUSD,
	)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

// Dispatch succeeds once. A duplicate caller must not send again under this ID.
func (s Service) Dispatch(ctx context.Context, id string) error {
	var enforced bool
	if err := s.DB.QueryRow(ctx, `SELECT enforced FROM credits.attempts WHERE id=$1`, id).Scan(&enforced); err != nil {
		return err
	}
	if enforced {
		return s.dispatchEnforced(ctx, id)
	}
	tag, err := s.DB.Exec(
		ctx,
		`UPDATE credits.attempts SET state='dispatched',dispatched_at=clock_timestamp() WHERE id=$1 AND state='reserved'`,
		id,
	)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

func (s Service) Settle(ctx context.Context, id string, value Settlement) error {
	var enforced bool
	if err := s.DB.QueryRow(ctx, `SELECT enforced FROM credits.attempts WHERE id=$1`, id).Scan(&enforced); err != nil {
		return err
	}
	if !enforced {
		return s.settle(ctx, s.DB, id, value)
	}
	var payer string
	if err := s.DB.QueryRow(ctx, `SELECT payer FROM credits.attempts WHERE id=$1`, id).Scan(&payer); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockPolicy(ctx, tx, payer); err != nil {
		return err
	}
	if err = s.settle(ctx, tx, id, value); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type executor interface {
	queryRow
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s Service) settle(ctx context.Context, db executor, id string, value Settlement) error {
	value, priceErr := priceUsage(ctx, db, id, value)
	if priceErr != nil {
		return priceErr
	}
	if err := value.Usage.Validate(); err != nil {
		return err
	}
	if value.CostBasis != basisUnknown && value.CostBasis != basisEstimated && value.CostBasis != "provider_reported" &&
		value.CostBasis != basisFree {
		return ErrInvalid
	}
	if value.CostBasis == basisUnknown && value.CostNanoUSD != nil {
		return ErrInvalid
	}
	if value.CostBasis != basisUnknown && (value.CostNanoUSD == nil || *value.CostNanoUSD < 0) {
		return ErrInvalid
	}
	if value.CostBasis == basisFree && *value.CostNanoUSD != 0 {
		return ErrInvalid
	}
	if len(value.PriceVersion) > 256 || len(value.ConversionVersion) > 256 || len(value.OriginalAmount) > 1024 ||
		len(value.OriginalCurrency) > 16 {
		return ErrInvalid
	}
	if value.CostBasis == basisEstimated && (value.PriceVersion == "" || value.ConversionVersion == "") {
		return ErrInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ErrInvalid
	}
	digest := sha256.Sum256(raw)
	tag, err := db.Exec(ctx, `UPDATE credits.attempts SET state='settled',usage=$2,usage_sha256=$3,
 cost_nano_usd=$4,cost_basis=$5,price_version=COALESCE(NULLIF($6,''),price_version),conversion_version=$7,settled_at=COALESCE(settled_at,clock_timestamp())
 WHERE id=$1 AND (state='dispatched' OR (state='settled' AND usage_sha256=$3))`, id, raw, hex.EncodeToString(digest[:]),
		value.CostNanoUSD, value.CostBasis, value.PriceVersion, value.ConversionVersion)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}
