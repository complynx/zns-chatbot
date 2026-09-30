package passbooking

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

// An owner can distinguish an unsubmitted payment from denied access. This
// does not authorize a receipt or manufacture a payment attempt.
func (s Service) absentOwnerPayment(
	ctx context.Context,
	owner, event string,
	payment Payment,
	failure error,
) (Payment, error) {
	problem, ok := errors.AsType[*core.ProblemError](failure)
	if !ok || problem.Status != http.StatusForbidden {
		return payment, failure
	}
	// Legacy metadata, including unknown dates, is evidence rather than absence.
	// Its failed attachment authorization must retain the original denial.
	absent, err := dbgen.New(s.DB).HasUnsubmittedPayment(ctx, dbgen.HasUnsubmittedPaymentParams{
		Event: event,
		Owner: owner,
	})
	if err != nil {
		return Payment{}, core.DatabaseOperationError(err)
	}
	if absent {
		return Payment{}, &core.ProblemError{
			Status: http.StatusNotFound,
			Code:   "pass_payment_missing",
		}
	}
	return payment, failure
}
