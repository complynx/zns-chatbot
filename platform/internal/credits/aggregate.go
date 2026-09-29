package credits

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type AggregateRow struct {
	Key                 string `json:"-"`
	Payer               string `json:"payer"`
	Operation           string `json:"operation"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	OperationKey        string `json:"operation_key"`
	Attempts            int64  `json:"attempts"`
	Cost                int64  `json:"cost_nano_usd"`
	Held                int64  `json:"held_nano_usd"`
	Unknown             int64  `json:"unknown"`
	ReconciliationDelta int64  `json:"reconciliation_delta_nano_usd"`
}

// Aggregate is an administrator-only, paged UTC-month report. Operation keys
// retain trusted campaign/subject attribution; unknown amounts have a count.
func (s Service) Aggregate(
	ctx context.Context,
	actor string,
	period time.Time,
	rawCursor string,
) (core.ReadPage[AggregateRow], error) {
	if period.IsZero() || !period.Equal(month(period)) {
		return core.ReadPage[AggregateRow]{}, ErrInvalid
	}
	if err := requireAdmin(ctx, s.DB, actor); err != nil {
		return core.ReadPage[AggregateRow]{}, err
	}
	cursor, err := core.DecodeReadCursor(rawCursor, actor, "credits.aggregate:"+period.Format(time.RFC3339))
	if err != nil {
		return core.ReadPage[AggregateRow]{}, err
	}
	rows, err := s.DB.Query(ctx, `WITH source AS (
 SELECT a.payer,a.operation,a.provider,a.model,a.operation_key,1::bigint AS attempts,
 COALESCE(r.cost_nano_usd,a.cost_nano_usd,0) AS cost,
 CASE WHEN COALESCE(r.cost_nano_usd,a.cost_nano_usd) IS NULL THEN COALESCE(a.reserved_nano_usd,0) ELSE 0 END AS held,
 CASE WHEN COALESCE(r.cost_nano_usd,a.cost_nano_usd) IS NULL THEN 1 ELSE 0 END AS unknown,
 CASE WHEN r.cost_nano_usd IS NOT NULL AND a.cost_nano_usd IS NOT NULL THEN r.cost_nano_usd-a.cost_nano_usd ELSE 0 END AS delta
 FROM credits.attempts a LEFT JOIN credits.reconciliations r ON r.attempt_id=a.id WHERE a.period_start=$1 AND a.state<>'not_sent'
 UNION ALL SELECT payer,'adjustment','','',operation_key,0,delta_nano_usd,0,0,0 FROM credits.adjustments WHERE period_start=$1
 ), grouped AS (
 SELECT jsonb_build_array(payer,operation,provider,model,operation_key)::text AS key,payer,operation,provider,model,operation_key,
 sum(attempts)::bigint AS attempts,sum(cost)::bigint AS cost,sum(held)::bigint AS held,sum(unknown)::bigint AS unknown,sum(delta)::bigint AS delta
 FROM source GROUP BY payer,operation,provider,model,operation_key)
 SELECT key,payer,operation,provider,model,operation_key,attempts,cost,held,unknown,delta FROM grouped WHERE key>$2 ORDER BY key LIMIT $3`, period, cursor.Position, core.ReadPageItems+1)
	if err != nil {
		return core.ReadPage[AggregateRow]{}, err
	}
	defer rows.Close()
	items := []AggregateRow{}
	for rows.Next() {
		var item AggregateRow
		if err = rows.Scan(
			&item.Key,
			&item.Payer,
			&item.Operation,
			&item.Provider,
			&item.Model,
			&item.OperationKey,
			&item.Attempts,
			&item.Cost,
			&item.Held,
			&item.Unknown,
			&item.ReconciliationDelta,
		); err != nil {
			return core.ReadPage[AggregateRow]{}, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return core.ReadPage[AggregateRow]{}, err
	}
	return core.NavigationPage(items, cursor, func(item AggregateRow) string { return item.Key })
}
