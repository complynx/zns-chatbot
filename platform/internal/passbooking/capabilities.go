package passbooking

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const capabilityPaymentAdmin = "payment_admin"
const capabilityCancel = "cancel"

// Capabilities lists only currently authorized operations for one event.
// Resource state and versions are still checked when an operation executes.
type Capabilities struct {
	Event   string   `json:"event"`
	Actions []string `json:"actions"`
}

func (s Service) Capabilities(ctx context.Context, actor, eventID string) (Capabilities, error) {
	result := Capabilities{Event: eventID, Actions: []string{}}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	returnValue, err := capabilitiesInTx(ctx, tx, actor, eventID)
	if err != nil {
		return result, err
	}
	return returnValue, tx.Commit(ctx)
}
func capabilitiesInTx(ctx context.Context, tx pgx.Tx, actor, eventID string) (Capabilities, error) {
	result := Capabilities{Event: eventID, Actions: []string{}}
	var err error
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_events WHERE id=$1)`, eventID).
		Scan(&exists); err != nil {
		return result, err
	}
	if !exists {
		return result, conflict("pass_event_unknown")
	}
	for _, name := range []string{solo, commandInvite, commandAccept, commandDecline, capabilityPaymentAdmin, capabilityCancel, commandAdminCancel, commandBatchUncouple, commandRecalculate, commandProofAccept, commandProofReject, commandAdminAssign, CommandTakeover, CommandReceivedOnly} {
		if _, err = authorize(ctx, tx, actor, name, eventID); err == nil {
			result.Actions = append(result.Actions, name)
		} else if problem, ok := errors.AsType[*core.ProblemError](err); !ok || problem.Status != http.StatusForbidden {
			return result, err
		}
	}
	return result, nil
}
