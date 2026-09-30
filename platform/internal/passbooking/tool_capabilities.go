package passbooking

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// ToolCapabilities is a discovery union only. Each event operation still checks
// its own current authority when it executes.
type ToolCapabilities struct {
	Actions []string `json:"actions"`
	Export  bool     `json:"export"`
}

func (s Service) ToolCapabilities(ctx context.Context, actor string) (ToolCapabilities, error) {
	result := ToolCapabilities{Actions: []string{}}
	if err := s.requireActor(ctx, actor); err != nil {
		return result, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Ordinary and booking-admin grants are event-independent. One payment-admin
	// event represents the additional event-scoped grant for discovery only.
	rows, err := tx.Query(ctx, `SELECT min(id) FROM core.pass_events HAVING count(*)>0
 UNION SELECT min(event_id) FROM core.pass_payment_admins WHERE owner=$1 HAVING count(*)>0`, actor)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	for _, event := range events {
		capability, readErr := capabilitiesInTx(ctx, tx, actor, event)
		if readErr != nil {
			return result, readErr
		}
		result.Actions = append(result.Actions, capability.Actions...)
	}
	slices.Sort(result.Actions)
	result.Actions = slices.Compact(result.Actions)
	err = tx.QueryRow(ctx, exportEvents+`SELECT EXISTS(SELECT 1 FROM allowed_events)`, actor).Scan(&result.Export)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}
