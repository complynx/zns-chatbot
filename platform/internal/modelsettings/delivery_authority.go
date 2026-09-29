package modelsettings

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// LockDeliveryReadInTx holds current capability rows through the send admission.
func LockDeliveryReadInTx(ctx context.Context, tx pgx.Tx, actor, capability string) error {
	if capability == GrantPermission {
		capability = ""
	}
	if capability != "" && capability != Own && capability != Others && capability != Global {
		return problem(http.StatusBadRequest, "invalid_model_settings")
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE) OR EXISTS(SELECT 1 FROM core.model_setting_grants WHERE owner=$1 AND capability=$2 FOR SHARE)`, actor, capability).
		Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return problem(http.StatusForbidden, "forbidden")
	}
	return nil
}
