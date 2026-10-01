package passbooking

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

// registrationTime is called at the original SQL observation point, after
// locks where required. The default retains its query and error behavior.
func registrationTime(ctx context.Context, tx pgx.Tx, clock registrationingress.Clock) (time.Time, error) {
	observed, configured, err := registrationingress.Observe(ctx, clock)
	if err != nil {
		return time.Time{}, err
	}
	if configured {
		return observed, nil
	}
	var now time.Time
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, core.DatabaseOperationError(err)
}

// registrationTurnTime observes configured time after the shared turn allocator
// is held. Subsequent rotation cannot wait past this observation.
func registrationTurnTime(ctx context.Context, tx pgx.Tx, clock registrationingress.Clock) (time.Time, error) {
	if clock != nil {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(782619)"); err != nil {
			return time.Time{}, core.DatabaseOperationError(err)
		}
	}
	return registrationTime(ctx, tx, clock)
}
