package credits

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s Service) Permissions(ctx context.Context, actor string) (map[string]bool, error) {
	allowed, err := superadmin(ctx, s.DB, actor)
	return map[string]bool{"admin": allowed}, err
}
func (s Service) DefaultPolicy(ctx context.Context, actor string) (Policy, error) {
	if err := requireAdmin(ctx, s.DB, actor); err != nil {
		return Policy{}, err
	}
	result := Policy{Payer: "*"}
	err := s.DB.QueryRow(ctx, `SELECT monthly_nano_usd,version FROM credits.default_policy WHERE singleton`).
		Scan(&result.MonthlyNanoUSD, &result.Version)
	return result, err
}
func setDefaultPolicy(ctx context.Context, tx pgx.Tx, actor string, input PolicyChange) (Policy, error) {
	if input.MonthlyNanoUSD == nil || input.Unlimited {
		return Policy{}, ErrInvalid
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(64001,0)`); err != nil {
		return Policy{}, err
	}
	request, _ := json.Marshal(struct {
		Payer  string       `json:"payer"`
		Change PolicyChange `json:"change"`
	}{"*", input})
	var same bool
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT (request #- '{change,version}')=($3::jsonb #- '{change,version}'),result FROM credits.policy_changes WHERE actor=$1 AND operation_key=$2`, actor, input.OperationKey, request).
		Scan(&same, &raw)
	if err == nil {
		if !same {
			return Policy{}, ErrConflict
		}
		var result Policy
		if json.Unmarshal(raw, &result) != nil {
			return Policy{}, ErrInvalid
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, err
	}
	result := Policy{Payer: "*"}
	err = tx.QueryRow(ctx, `UPDATE credits.default_policy SET monthly_nano_usd=$1,version=version+1 WHERE singleton AND version=$2 RETURNING monthly_nano_usd,version`, input.MonthlyNanoUSD, input.Version).
		Scan(&result.MonthlyNanoUSD, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, ErrConflict
	}
	if err != nil {
		return Policy{}, err
	}
	raw, _ = json.Marshal(result)
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO credits.policy_changes(actor,operation_key,request,result) VALUES($1,$2,$3,$4)`,
		actor,
		input.OperationKey,
		request,
		raw,
	); err != nil {
		return Policy{}, err
	}
	return result, tx.Commit(ctx)
}
