package credits

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

type PreparedPolicy struct {
	tx           pgx.Tx
	actor, payer string
	input        PolicyChange
	request      []byte
	replay       Policy
	found        bool
}

func validPolicyChange(payer string, input PolicyChange) error {
	if payer == "" || len(payer) > 256 || input.Version < 1 || input.OperationKey == "" ||
		len(input.OperationKey) > 128 ||
		(input.MonthlyNanoUSD != nil && *input.MonthlyNanoUSD < 0) {
		return ErrInvalid
	}
	return nil
}

// PreparePolicyInTx retains current target authority and serialization locks.
// Account materialization and policy writes occur only in Apply.
func (s Service) PreparePolicyInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, payer string,
	input PolicyChange,
) (*PreparedPolicy, error) {
	if err := validPolicyChange(payer, input); err != nil {
		return nil, err
	}
	if err := requireAdmin(ctx, tx, actor); err != nil {
		return nil, err
	}
	if payer == "*" && (input.MonthlyNanoUSD == nil || input.Unlimited) {
		return nil, ErrInvalid
	}
	if input.MonthlyNanoUSD != nil {
		amount := *input.MonthlyNanoUSD
		input.MonthlyNanoUSD = &amount
	}
	if _, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,64001))`,
		actor+":"+input.OperationKey,
	); err != nil {
		return nil, err
	}
	lock := `SELECT pg_advisory_xact_lock_shared(64001,0)`
	if payer == "*" {
		lock = `SELECT pg_advisory_xact_lock(64001,0)`
	}
	if _, err := tx.Exec(ctx, lock); err != nil {
		return nil, err
	}
	request, _ := json.Marshal(struct {
		Payer  string       `json:"payer"`
		Change PolicyChange `json:"change"`
	}{payer, input})
	p := &PreparedPolicy{tx: tx, actor: actor, payer: payer, input: input, request: request}
	var same bool
	var result []byte
	err := tx.QueryRow(ctx, `SELECT (request #- '{change,version}')=($3::jsonb #- '{change,version}'),result FROM credits.policy_changes WHERE actor=$1 AND operation_key=$2`, actor, input.OperationKey, request).
		Scan(&same, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if !same {
		return nil, ErrConflict
	}
	if json.Unmarshal(result, &p.replay) != nil {
		return nil, ErrInvalid
	}
	p.found = true
	return p, nil
}

func (p *PreparedPolicy) Replay() (Policy, bool) { return p.replay, p.found }

func (p *PreparedPolicy) Apply(ctx context.Context) (Policy, error) {
	if p.found {
		return p.replay, nil
	}
	var policy Policy
	var err error
	if p.payer == "*" {
		policy.Payer = "*"
		err = p.tx.QueryRow(ctx, `UPDATE credits.default_policy SET monthly_nano_usd=$1,version=version+1 WHERE singleton AND version=$2 RETURNING monthly_nano_usd,version`, p.input.MonthlyNanoUSD, p.input.Version).
			Scan(&policy.MonthlyNanoUSD, &policy.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return Policy{}, ErrConflict
		}
	} else {
		policy, err = p.applyAccount(ctx)
	}
	if err != nil {
		return Policy{}, err
	}
	result, _ := json.Marshal(policy)
	_, err = p.tx.Exec(
		ctx,
		`INSERT INTO credits.policy_changes(actor,operation_key,request,result) VALUES($1,$2,$3,$4)`,
		p.actor,
		p.input.OperationKey,
		p.request,
		result,
	)
	return policy, err
}

func (p *PreparedPolicy) applyAccount(ctx context.Context) (Policy, error) {
	if _, err := lockPolicy(ctx, p.tx, p.payer); err != nil {
		return Policy{}, err
	}
	tag, err := p.tx.Exec(
		ctx,
		`UPDATE credits.accounts SET monthly_nano_usd=$2,unlimited=$3,version=version+1 WHERE payer=$1 AND version=$4`,
		p.payer,
		p.input.MonthlyNanoUSD,
		p.input.Unlimited,
		p.input.Version,
	)
	if err != nil {
		return Policy{}, err
	}
	if tag.RowsAffected() != 1 {
		return Policy{}, ErrConflict
	}
	return lockPolicy(ctx, p.tx, p.payer)
}
