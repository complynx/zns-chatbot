package conversation

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func checkHistoryReadBudget(ctx context.Context, tx pgx.Tx, actor string, ids []int64, summary, textOnly bool) error {
	if len(ids) == 0 && !summary {
		return nil
	}
	allowed, err := dbgen.New(tx).HistoryReadWithinBudget(ctx, dbgen.HistoryReadWithinBudgetParams{
		Owner: actor, EventIds: ids, IncludeSummary: summary, TextOnly: textOnly, Budget: core.ReadResourceBytes,
	})
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !allowed {
		return core.ReadProblem("read_result_limit")
	}
	return nil
}

// Input is already bounded before database materialization. This final check
// includes JSON escaping, metadata and repeated evidence without a wire roundtrip.
func boundedHistoryResult[T any](value T, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return zero, err
	}
	if len(data) > core.ReadResourceBytes {
		return zero, core.ReadProblem("read_result_limit")
	}
	return value, nil
}
