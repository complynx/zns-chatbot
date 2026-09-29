package credits

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

var ErrLimit = errors.New("credits_limit_reached")
var ErrUnpriced = errors.New("credits_safe_bound_unavailable")

type Policy struct {
	Payer             string `json:"payer"`
	MonthlyNanoUSD    int64  `json:"monthly_nano_usd"`
	Unlimited         bool   `json:"unlimited"`
	ImplicitUnlimited bool   `json:"implicit_unlimited"`
	Version           int64  `json:"version"`
	Inherited         bool   `json:"inherited"`
}
type PolicyChange struct {
	MonthlyNanoUSD *int64 `json:"monthly_nano_usd"`
	Unlimited      bool   `json:"unlimited"`
	Version        int64  `json:"version"`
	OperationKey   string `json:"operation_key"`
}
type UsageReport struct {
	Policy           Policy    `json:"policy"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	SpentNanoUSD     int64     `json:"spent_nano_usd"`
	HeldNanoUSD      int64     `json:"held_nano_usd"`
	UnboundedUnknown int64     `json:"unbounded_unknown"`
	AvailableNanoUSD *int64    `json:"available_nano_usd"`
}

type queryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func superadmin(ctx context.Context, db queryRow, actor string) (bool, error) {
	var yes bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1)`, actor).Scan(&yes)
	return yes, err
}
func requireAdmin(ctx context.Context, db queryRow, actor string) error {
	yes, err := superadmin(ctx, db, actor)
	if err != nil {
		return err
	}
	if !yes {
		return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return nil
}
func month(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func lockPolicy(ctx context.Context, tx pgx.Tx, payer string) (Policy, error) {
	// Shared advisory locking serializes policy changes without granting paid
	// workers UPDATE permission on the default policy table.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(64001,0)`); err != nil {
		return Policy{}, err
	}
	var fallback int64
	if err := tx.QueryRow(ctx, `SELECT monthly_nano_usd FROM credits.default_policy WHERE singleton`).
		Scan(&fallback); err != nil {
		return Policy{}, err
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO credits.accounts(payer) VALUES($1) ON CONFLICT DO NOTHING`,
		payer,
	); err != nil {
		return Policy{}, err
	}
	result := Policy{Payer: payer}
	var own *int64
	err := tx.QueryRow(ctx, `SELECT monthly_nano_usd,unlimited,version FROM credits.accounts WHERE payer=$1 FOR UPDATE`, payer).
		Scan(&own, &result.Unlimited, &result.Version)
	if err != nil {
		return result, err
	}
	result.Inherited = own == nil && !result.Unlimited
	result.MonthlyNanoUSD = fallback
	if own != nil {
		result.MonthlyNanoUSD = *own
	}
	result.ImplicitUnlimited, err = superadmin(ctx, tx, payer)
	result.Unlimited = result.Unlimited || result.ImplicitUnlimited
	return result, err
}

func usageAt(ctx context.Context, db queryRow, policy Policy, period time.Time) (UsageReport, error) {
	result := UsageReport{Policy: policy, PeriodStart: period, PeriodEnd: period.AddDate(0, 1, 0)}
	err := db.QueryRow(ctx, `SELECT
 (COALESCE(sum(COALESCE(r.cost_nano_usd,a.cost_nano_usd)) FILTER(WHERE a.period_start=$2 AND a.state<>'not_sent'),0)
 +COALESCE((SELECT sum(delta_nano_usd) FROM credits.adjustments WHERE payer=$1 AND period_start=$2),0))::bigint,
 COALESCE(sum(a.reserved_nano_usd) FILTER(WHERE a.period_start=$2 AND COALESCE(r.cost_nano_usd,a.cost_nano_usd) IS NULL AND a.state<>'not_sent'),0)::bigint,
 count(*) FILTER(WHERE COALESCE(r.cost_nano_usd,a.cost_nano_usd) IS NULL AND a.reserved_nano_usd IS NULL AND a.state<>'not_sent')
 FROM credits.attempts a LEFT JOIN credits.reconciliations r ON r.attempt_id=a.id WHERE a.payer=$1`, policy.Payer, period).Scan(&result.SpentNanoUSD, &result.HeldNanoUSD, &result.UnboundedUnknown)
	if err != nil {
		return result, err
	}
	if !policy.Unlimited {
		available := int64(0)
		remaining := creditRemaining(policy.MonthlyNanoUSD, result.SpentNanoUSD, result.HeldNanoUSD)
		if remaining.Sign() > 0 && result.UnboundedUnknown == 0 {
			if !remaining.IsInt64() {
				return result, ErrInvalid
			}
			available = remaining.Int64()
		}
		result.AvailableNanoUSD = &available
	}
	return result, nil
}

func creditRemaining(limit, spent, held int64) *big.Int {
	result := new(big.Int).Sub(big.NewInt(limit), big.NewInt(spent))
	return result.Sub(result, big.NewInt(held))
}

func (s Service) Usage(ctx context.Context, actor, payer string) (UsageReport, error) {
	if actor == "" || payer == "" || len(actor) > 256 || len(payer) > 256 {
		return UsageReport{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return UsageReport{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if actor != payer {
		if err = requireAdmin(ctx, tx, actor); err != nil {
			return UsageReport{}, err
		}
	}
	policy, err := lockPolicy(ctx, tx, payer)
	if err != nil {
		return UsageReport{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return UsageReport{}, err
	}
	result, err := usageAt(ctx, tx, policy, month(now))
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// SetPolicy fences a new write by version. A matching business-operation replay
// may carry a refreshed version after a lost reply; it returns the saved result.
func (s Service) SetPolicy(ctx context.Context, actor, payer string, input PolicyChange) (Policy, error) {
	if payer == "" || len(payer) > 256 || input.Version < 1 || input.OperationKey == "" ||
		len(input.OperationKey) > 128 ||
		(input.MonthlyNanoUSD != nil && *input.MonthlyNanoUSD < 0) {
		return Policy{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Policy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = requireAdmin(ctx, tx, actor); err != nil {
		return Policy{}, err
	}
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,64001))`,
		actor+":"+input.OperationKey,
	); err != nil {
		return Policy{}, err
	}
	if payer == "*" {
		return setDefaultPolicy(ctx, tx, actor, input)
	}
	if _, err = lockPolicy(ctx, tx, payer); err != nil {
		return Policy{}, err
	}
	request, _ := json.Marshal(struct {
		Payer  string       `json:"payer"`
		Change PolicyChange `json:"change"`
	}{payer, input})
	var same bool
	var result []byte
	err = tx.QueryRow(ctx, `SELECT (request #- '{change,version}')=($3::jsonb #- '{change,version}'),result FROM credits.policy_changes WHERE actor=$1 AND operation_key=$2`, actor, input.OperationKey, request).
		Scan(&same, &result)
	if err == nil {
		if !same {
			return Policy{}, ErrConflict
		}
		var policy Policy
		if json.Unmarshal(result, &policy) != nil {
			return Policy{}, ErrInvalid
		}
		return policy, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, err
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE credits.accounts SET monthly_nano_usd=$2,unlimited=$3,version=version+1 WHERE payer=$1 AND version=$4`,
		payer,
		input.MonthlyNanoUSD,
		input.Unlimited,
		input.Version,
	)
	if err != nil {
		return Policy{}, err
	}
	if tag.RowsAffected() != 1 {
		return Policy{}, ErrConflict
	}
	policy, err := lockPolicy(ctx, tx, payer)
	if err != nil {
		return Policy{}, err
	}
	result, _ = json.Marshal(policy)
	_, err = tx.Exec(
		ctx,
		`INSERT INTO credits.policy_changes(actor,operation_key,request,result) VALUES($1,$2,$3,$4)`,
		actor,
		input.OperationKey,
		request,
		result,
	)
	if err != nil {
		return Policy{}, err
	}
	return policy, tx.Commit(ctx)
}
