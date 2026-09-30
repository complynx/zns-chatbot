package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Host) PassPaymentReceipt(
	ctx context.Context,
	owner string,
	command passbooking.Command,
	source readsource.Derivation,
) (passbooking.PaymentCompletionReceipt, error) {
	var result passbooking.PaymentCompletionReceipt
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (passbooking.PaymentCompletionReceipt, error) {
					return s.PassPaymentReceipt(ctx, actor, command, source)
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-payments/receipt", command, source, &result)
	return registrationHTTPResult(result, err)
}
