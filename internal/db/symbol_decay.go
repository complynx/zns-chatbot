package db

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/symbols"
)

const defaultSymbolMinHalfLife = 60 * 60
const defaultSymbolMaxHalfLife = 180 * 24 * 60 * 60
const minimumSymbolIntervalSamples = 10
const maximumSymbolMedianMultiplier = 1000
const minimumSymbolMedianMultiplier = 0.01

type symbolDecayPolicy struct {
	multiplier       float64
	minimum, maximum int
}

func (d Settings) symbolDecayPolicy(fallback int) (symbolDecayPolicy, error) {
	p := symbolDecayPolicy{d.SymbolMedianMultiplier, d.SymbolMinHalfLifeSeconds, d.SymbolMaxHalfLifeSeconds}
	if p.multiplier == 0 {
		p.multiplier = 1
	}
	if p.minimum == 0 {
		p.minimum = defaultSymbolMinHalfLife
	}
	if p.maximum == 0 {
		p.maximum = defaultSymbolMaxHalfLife
	}
	if math.IsNaN(p.multiplier) || math.IsInf(p.multiplier, 0) || p.multiplier < minimumSymbolMedianMultiplier ||
		p.multiplier > maximumSymbolMedianMultiplier || p.minimum < 60 || p.maximum > 365*24*60*60 ||
		p.maximum < p.minimum || fallback < p.minimum || fallback > p.maximum {
		return p, errors.New("invalid visual symbol median decay policy")
	}
	return p, nil
}

func symbolHalfLives(
	ctx context.Context,
	tx pgx.Tx,
	bot int64,
	task uuid.UUID,
	d Settings,
	fallback int,
) (map[string]int, error) {
	p, err := d.symbolDecayPolicy(fallback)
	if err != nil {
		return nil, err
	}
	result := map[string]int{}
	for _, kind := range []string{symbols.Sticker, symbols.Emoji} {
		half, halfErr := catalogHalfLife(ctx, tx, bot, task, kind, p, fallback)
		if halfErr != nil {
			return nil, halfErr
		}
		result[kind] = half
	}
	return result, nil
}

// One positive latest reuse interval per retained symbol keeps one very busy
// sticker from dominating the median. Never-used and within-message gaps are excluded.
func medianSymbolHalfLife(
	ctx context.Context,
	tx pgx.Tx,
	bot int64,
	kind string,
	p symbolDecayPolicy,
	fallback int,
) (int, error) {
	var samples int
	var median float64
	err := tx.QueryRow(ctx, `SELECT count(*),coalesce(percentile_cont(0.5) WITHIN GROUP
 (ORDER BY last_interval_seconds),0) FROM shuttle_symbols
WHERE bot_id=$1 AND kind=$2 AND last_interval_seconds>0`, bot, kind).Scan(&samples, &median)
	if err != nil {
		return 0, err
	}
	if samples < minimumSymbolIntervalSamples {
		return fallback, nil
	}
	return int(math.Round(math.Max(float64(p.minimum), math.Min(float64(p.maximum), p.multiplier*median)))), nil
}

func catalogHalfLife(
	ctx context.Context,
	tx pgx.Tx,
	bot int64,
	task uuid.UUID,
	kind string,
	p symbolDecayPolicy,
	fallback int,
) (int, error) {
	var previous int
	var fresh bool
	policy, _ := json.Marshal(struct {
		Multiplier float64 `json:"multiplier"`
		Minimum    int     `json:"minimum"`
		Maximum    int     `json:"maximum"`
		Fallback   int     `json:"fallback"`
	}{p.multiplier, p.minimum, p.maximum, fallback})
	err := tx.QueryRow(ctx, `SELECT half_life_seconds,
evaluated_at>transaction_timestamp()-interval '1 hour' AND policy=$3::jsonb
FROM shuttle_symbol_decay WHERE bot_id=$1 AND kind=$2`, bot, kind, policy).Scan(&previous, &fresh)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if fresh && previous >= p.minimum && previous <= p.maximum {
		return previous, nil
	}
	half, err := medianSymbolHalfLife(ctx, tx, bot, kind, p, fallback)
	if err != nil {
		return 0, err
	}
	if previous != 0 && previous != half {
		if err = reweightSymbolScores(ctx, tx, bot, task, kind, half); err != nil {
			return 0, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_symbol_decay(bot_id,kind,half_life_seconds,evaluated_at,policy)
VALUES($1,$2,$3,transaction_timestamp(),$4) ON CONFLICT(bot_id,kind) DO UPDATE SET
half_life_seconds=EXCLUDED.half_life_seconds,evaluated_at=EXCLUDED.evaluated_at,
policy=EXCLUDED.policy`, bot, kind, half, policy)
	return half, err
}

// Reweight only at a policy change, from already retained immutable inputs. This
// keeps cache hits and re-admitted misses comparable under the new shared half-life.
func reweightSymbolScores(ctx context.Context, tx pgx.Tx, bot int64, task uuid.UUID, kind string, half int) error {
	_, err := tx.Exec(ctx, `WITH weights AS (
 SELECT s.asset_key,sum(shuttle_symbol_weight(1.0,extract(epoch FROM
 (s.last_used_at-t.created_at))::float8,$4))::float8 AS weight
 FROM shuttle_tasks t JOIN shuttle_users u ON u.id=t.user_id
 CROSS JOIN LATERAL jsonb_array_elements(t.request->'symbols') o
 JOIN shuttle_symbols s ON s.bot_id=u.bot_id AND s.kind=o->>'kind' AND s.asset_key=o->>'id'
 WHERE u.bot_id=$1 AND s.kind=$2 AND t.id<>$3 AND t.request ? 'symbols'
 GROUP BY s.asset_key
) UPDATE shuttle_symbols s SET score=greatest(w.weight,1e-100)
FROM weights w WHERE s.bot_id=$1 AND s.kind=$2 AND s.asset_key=w.asset_key`, bot, kind, task, half)
	return err
}
