package passes

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// HistoryPage contains bounded owner-only metadata, newest first.
type HistoryPage struct {
	Items      []Change `json:"items"`
	NextBefore int64    `json:"next_before,omitempty"`
}

func (s Service) HistoryPage(ctx context.Context, actor string, before int64) (HistoryPage, error) {
	if before < 0 {
		return HistoryPage{}, problem(http.StatusBadRequest, "invalid_cursor")
	}
	if _, err := s.Get(ctx, actor); err != nil {
		return HistoryPage{}, err
	}
	const limit = 20
	rows, err := s.DB.Query(ctx, `SELECT version,action,field,origin,at FROM core.pass_profile_history
 WHERE owner=$1 AND ($2::bigint=0 OR version<$2) ORDER BY version DESC LIMIT $3`, actor, before, limit+1)
	if err != nil {
		return HistoryPage{}, core.DatabaseOperationError(err)
	}
	changes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Change])
	if err != nil {
		return HistoryPage{}, core.DatabaseOperationError(err)
	}
	page := HistoryPage{Items: changes}
	if len(changes) > limit {
		page.Items = changes[:limit]
		page.NextBefore = page.Items[limit-1].Version
	}
	return page, nil
}
