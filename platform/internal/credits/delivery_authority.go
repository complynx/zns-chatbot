package credits

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// LockDeliveryReadInTx retains the same administrator right as Permissions.
func LockDeliveryReadInTx(ctx context.Context, tx pgx.Tx, actor, target string) error {
	if target == "" || target == actor {
		return nil
	}
	var found string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return err
}
