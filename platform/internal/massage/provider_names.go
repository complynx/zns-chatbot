package massage

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// ProviderNames returns public display names without staff preferences or clients.
func (s Service) ProviderNames(ctx context.Context, actor, event string) (map[string]string, error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT owner,name FROM core.massage_specialists WHERE event_id=$1`, event)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var owner, name string
		if err = rows.Scan(&owner, &name); err != nil {
			return nil, core.DatabaseOperationError(err)
		}
		names[owner] = name
	}
	return names, core.DatabaseOperationError(rows.Err())
}
