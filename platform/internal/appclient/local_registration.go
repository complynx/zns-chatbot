package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

// LocalRegistration authenticates each registration operation in the combined app.
// The domain service retains its transaction and current permission checks.
type LocalRegistration struct {
	Service    passbooking.Service
	Profile    passes.Service
	Files      orders.Service
	Batches    derivedmutation.Service
	Authorizer applicationauth.Authorizer
}

func (c Client) registrationActor(ctx context.Context, owner string) (string, error) {
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return "", err
	}
	return authorizeOwner(ctx, c.LocalRegistration.Authorizer, token, owner)
}

func (c Client) localPassBooking(ctx context.Context, owner, event string) (passbooking.Booking, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return passbooking.Booking{}, err
	}
	return registrationResult(c.LocalRegistration.Service.Get(ctx, actor, event))
}

func (c Client) localExecutePassBooking(
	ctx context.Context, owner string, command passbooking.Command,
) (passbooking.Booking, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return passbooking.Booking{}, err
	}
	return registrationResult(c.LocalRegistration.Service.Execute(ctx, actor, command))
}

func registrationResult(value passbooking.Booking, err error) (passbooking.Booking, error) {
	if err != nil {
		return passbooking.Booking{}, orderApplicationError(err)
	}
	return value, nil
}
