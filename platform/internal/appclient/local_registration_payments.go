package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) localPassPayment(ctx context.Context, actor, event, owner string) (passbooking.Payment, error) {
	return directRegistration(
		ctx,
		c,
		actor,
		func(service passbooking.Service, verified string) (passbooking.Payment, error) {
			return service.Payment(ctx, verified, event, owner)
		},
	)
}

func (c Client) localPassPaymentQuote(ctx context.Context, owner, event string) (passbooking.PaymentQuote, error) {
	return directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (passbooking.PaymentQuote, error) {
			return service.PaymentQuote(ctx, actor, event)
		},
	)
}

func (c Client) localPassPaymentQueue(
	ctx context.Context,
	owner, event, after string,
) (passbooking.PaymentPage, error) {
	return directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (passbooking.PaymentPage, error) {
			return service.PaymentQueue(ctx, actor, event, after)
		},
	)
}
