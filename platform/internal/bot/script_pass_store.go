package bot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const registrationOperationRows = `SELECT call.value->'pass' AS request, call.value->'outcome' AS outcome, i.created_at
 FROM bot.interactions i CROSS JOIN LATERAL jsonb_array_elements(i.content) run
 CROSS JOIN LATERAL jsonb_array_elements(run->'calls') call(value)
 WHERE i.owner=$1 AND i.kind='script_runs' AND call.value->'pass' IS NOT NULL`

func (b *Bot) loadPassOperation(ctx context.Context, owner, id string) (*scriptPassRequest, error) {
	const operationIDLength = 26
	if len(id) != operationIDLength {
		return nil, errors.New("invalid pass operation reference")
	}
	var request scriptPassRequest
	err := b.DB.QueryRow(ctx, `SELECT request FROM (`+registrationOperationRows+`) operations WHERE request->>'id'=$2 ORDER BY created_at DESC LIMIT 1`, owner, id).
		Scan(&request)
	if err != nil {
		return nil, err
	}
	return &request, b.authorizePassRequest(ctx, owner, &request)
}

type scriptPassOperation struct {
	ID   string `json:"operation_id"`
	Name string `json:"tool"`
}

func (b *Bot) passOperations(ctx context.Context, owner string) (any, error) {
	rows, err := b.DB.Query(ctx, `SELECT request->>'id',request->>'name' FROM (
 SELECT DISTINCT ON(request->>'id') request,created_at FROM (`+registrationOperationRows+`) operations
 ORDER BY request->>'id',created_at DESC) recent ORDER BY created_at DESC LIMIT 20`, owner)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[scriptPassOperation])
}

func bindPassToolKey(request *scriptPassRequest, key string) {
	if request == nil {
		return
	}
	if request.Command != nil && request.Command.Key == "" {
		request.Command.Key = key
	}
	if request.Assignment != nil && request.Assignment.Key == "" {
		request.Assignment.Key = key
	}
	if request.Batch != nil && request.Batch.Key == "" {
		request.Batch.Key = key
	}
}
