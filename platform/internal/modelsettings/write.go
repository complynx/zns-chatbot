package modelsettings

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Enforce the prepared version at the mutation, including writers outside our advisory lock.
func write(ctx context.Context, tx pgx.Tx, actor, scope string, input Change) error {
	query := `UPDATE core.model_settings SET model=$2,effort=$3,version=version+1,author=$4,authority=$5
 WHERE scope=$1 AND version=$6`
	if input.Version == 0 {
		query = `INSERT INTO core.model_settings(scope,model,effort,version,author,authority)
 VALUES($1,$2,$3,$6+1,$4,$5) ON CONFLICT(scope) DO NOTHING`
	}
	result, err := tx.Exec(ctx, query, scope, input.Model, input.Effort, actor, permission(actor, scope), input.Version)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return problem(http.StatusConflict, "stale_model_settings")
	}
	return nil
}
