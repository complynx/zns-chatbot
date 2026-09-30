package orders

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"
)

type PaymentAdmin struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

func (s Service) PaymentAdmins(ctx context.Context, event string) ([]PaymentAdmin, error) {
	rows, err := s.DB.Query(ctx, `SELECT u.id,u.name,a.country FROM core.order_admins a
		JOIN core.users u ON u.id=a.owner WHERE a.event_id=$1 ORDER BY u.id`, event)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	admins, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PaymentAdmin])
	return admins, core.DatabaseOperationError(err)
}

func (s Service) authorizeInbox(ctx context.Context, actor, event string) error {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_admins a JOIN core.users u ON u.id=a.owner
		WHERE a.owner=$1 AND a.event_id=$2 AND u.can_book)`, actor, event).Scan(&allowed)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !allowed {
		return problem(http.StatusForbidden, "forbidden")
	}
	return nil
}
