package bot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

type foodPendingReader struct{ db *pgxpool.Pool }

func (r foodPendingReader) Read(ctx context.Context, owner string) (legacyfood.Command, bool, error) {
	var command legacyfood.Command
	err := r.db.QueryRow(ctx, `SELECT command FROM bot.food_pending WHERE owner=$1 AND expires_at>now()`, owner).
		Scan(&command)
	if errors.Is(err, pgx.ErrNoRows) {
		return command, false, nil
	}
	return command, err == nil, err
}
