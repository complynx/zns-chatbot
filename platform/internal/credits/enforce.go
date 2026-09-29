package credits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Service) reserveEnforced(ctx context.Context, a Attempt) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, err := lockPolicy(ctx, tx, a.Scope.Payer)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return ErrInvalid
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var prior *string
	err = tx.QueryRow(ctx, `SELECT request_sha256 FROM credits.attempts WHERE id=$1`, a.ID).Scan(&prior)
	if err == nil {
		if prior == nil || *prior != hash {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	amount, version, err := reserveBound(ctx, tx, a)
	if err != nil && !policy.Unlimited {
		return ErrUnpriced
	}
	if err = checkBudget(ctx, tx, policy, month(now), amount); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO credits.attempts(id,operation_key,actor,payer,operation,provider,model,state,reserved_nano_usd,price_version,period_start,enforced,request_sha256)
 VALUES($1,$2,$3,$4,$5,$6,$7,'reserved',$8,$9,$10,true,$11)`,
		a.ID,
		a.Scope.Key,
		a.Scope.Actor,
		a.Scope.Payer,
		a.Operation,
		a.Provider,
		a.Model,
		amount,
		version,
		month(now),
		hash,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func reserveBound(ctx context.Context, db queryRow, a Attempt) (*int64, *string, error) {
	if a.Provider != "openai" {
		return nil, nil, ErrUnpriced
	}
	var version string
	var raw []byte
	err := db.QueryRow(ctx, `SELECT p.version,p.specification FROM credits.price_selection s JOIN credits.price_versions p ON p.version=s.version WHERE s.provider=$1 AND s.model=$2`, a.Provider, a.Model).
		Scan(&version, &raw)
	if err != nil {
		return nil, nil, err
	}
	var price Price
	if json.Unmarshal(raw, &price) != nil {
		return nil, &version, ErrInvalid
	}
	amount, err := price.MaximumExposure(a.OutputLimit)
	if a.Operation == "audio.transcribe" {
		amount, err = price.MaximumASRExposure()
	}
	if err != nil {
		return nil, &version, err
	}
	return &amount, &version, nil
}

func checkBudget(ctx context.Context, db queryRow, policy Policy, period time.Time, amount *int64) error {
	if policy.Unlimited {
		return nil
	}
	if amount == nil {
		return ErrUnpriced
	}
	usage, err := usageAt(ctx, db, policy, period)
	if err != nil {
		return err
	}
	if usage.UnboundedUnknown > 0 || usage.AvailableNanoUSD == nil || *amount > *usage.AvailableNanoUSD {
		return ErrLimit
	}
	return nil
}

func (s Service) dispatchEnforced(ctx context.Context, id string) error {
	var payer string
	if err := s.DB.QueryRow(ctx, `SELECT payer FROM credits.attempts WHERE id=$1`, id).Scan(&payer); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, err := lockPolicy(ctx, tx, payer)
	if err != nil {
		return err
	}
	var period time.Time
	var amount *int64
	var state string
	if err = tx.QueryRow(ctx, `SELECT period_start,reserved_nano_usd,state FROM credits.attempts WHERE id=$1 FOR UPDATE`, id).
		Scan(&period, &amount, &state); err != nil {
		return err
	}
	if state != "reserved" {
		return ErrConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if !period.Equal(month(now)) {
		return releaseUnsent(ctx, tx, id, ErrConflict)
	}
	if !policy.Unlimited {
		if amount == nil {
			return releaseUnsent(ctx, tx, id, ErrUnpriced)
		}
		usage, readErr := usageAt(ctx, tx, policy, period)
		if readErr != nil {
			return readErr
		}
		if usage.UnboundedUnknown > 0 ||
			creditRemaining(policy.MonthlyNanoUSD, usage.SpentNanoUSD, usage.HeldNanoUSD).Sign() < 0 {
			return releaseUnsent(ctx, tx, id, ErrLimit)
		}
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE credits.attempts SET state='dispatched',dispatched_at=clock_timestamp() WHERE id=$1`,
		id,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func releaseUnsent(ctx context.Context, tx pgx.Tx, id string, reason error) error {
	if _, err := tx.Exec(
		ctx,
		`UPDATE credits.attempts SET state='not_sent' WHERE id=$1 AND state='reserved'`,
		id,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return reason
}
