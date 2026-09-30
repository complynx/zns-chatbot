package interaction

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// RegistrationReceiptObservation keeps fresh authority until model projection.
type RegistrationReceiptObservation struct {
	ID              string                 `json:"operation_id"`
	Complete        bool                   `json:"complete"`
	Result          passbooking.Booking    `json:"result"`
	ReadAuthorities []readsource.Authority `json:"read_authorities,omitempty"`
}

// ObserveReceipt reselects the exact owner admission without old outcomes or source.
// The domain proves commitment and reads current state in one locked transaction.
func (c RegistrationOperations) ObserveReceipt(
	ctx context.Context, owner, id string,
) (RegistrationReceiptObservation, error) {
	denied := &core.ProblemError{Status: http.StatusNotFound, Code: "pass_operation_unavailable"}
	if id == "" {
		return RegistrationReceiptObservation{}, denied
	}
	if err := (derivedmutation.PassOperationQuery{ID: id}).Validate(); err != nil {
		return RegistrationReceiptObservation{}, err
	}
	admitted, err := c.Ledger.ReadRegistrationOperations(ctx, owner, id)
	if err != nil {
		return RegistrationReceiptObservation{}, err
	}
	if len(admitted) != 1 || admitted[0].ID != id || !admitted[0].Retired ||
		admitted[0].Tool != "passes.registration.invite" {
		return RegistrationReceiptObservation{}, denied
	}
	input := registrationOperationInput(admitted[0])
	input.Source = nil
	input.ObserveOwnerBooking = true
	value, err := c.Domain.ReadPassOperation(ctx, owner, input)
	if err != nil {
		return RegistrationReceiptObservation{}, err
	}
	if value.Summary.Status != "committed" || value.CurrentBooking == nil ||
		value.CurrentBooking.Owner != owner || len(value.ReadAuthorities) == 0 {
		return RegistrationReceiptObservation{}, denied
	}
	return RegistrationReceiptObservation{ID: id, Complete: true,
		Result: *value.CurrentBooking, ReadAuthorities: value.ReadAuthorities}, nil
}
