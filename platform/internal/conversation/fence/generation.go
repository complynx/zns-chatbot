// Package fence serializes history-derived writes with owner history deletion.
package fence

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// CurrentGeneration reads under the caller's existing history lock.
func CurrentGeneration(ctx context.Context, tx pgx.Tx, actor string) (int64, error) {
	return dbgen.New(tx).HistoryGeneration(ctx, actor)
}

// LockGeneration serializes a derived write with history deletion until its
// transaction ends. The caller must first authorize the actor.
func LockGeneration(ctx context.Context, tx pgx.Tx, actor string, generation *int64) error {
	if generation == nil {
		return nil
	}
	// Use deletion's first lock before checking the generation or inserting an
	// event. Keep it through body insertion and commit.
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.conversation_summaries(owner) VALUES($1) ON CONFLICT DO NOTHING`,
		actor,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		`SELECT version FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`,
		actor,
	); err != nil {
		return err
	}
	current, readErr := dbgen.New(tx).HistoryGeneration(ctx, actor)
	if readErr != nil {
		return readErr
	}
	if current != *generation {
		return &core.ProblemError{Status: http.StatusConflict, Code: "history_stale"}
	}
	return nil
}
