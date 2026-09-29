package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) localPassEvents(ctx context.Context, owner string) ([]passbooking.Event, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return nil, err
	}
	value, err := c.LocalRegistration.Service.Events(ctx, actor)
	if err != nil {
		return nil, orderApplicationError(err)
	}
	return value, nil
}

func (c Client) localPassInvitations(
	ctx context.Context,
	owner, event, after string,
) (passbooking.InvitationPage, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return passbooking.InvitationPage{}, err
	}
	value, err := c.LocalRegistration.Service.Invitations(ctx, actor, event, after)
	if err != nil {
		return passbooking.InvitationPage{}, orderApplicationError(err)
	}
	return value, nil
}

func (c Client) localPassPaymentAdmins(ctx context.Context, owner, event string) ([]passbooking.Contact, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return nil, err
	}
	value, err := c.LocalRegistration.Service.PaymentAdmins(ctx, actor, event)
	if err != nil {
		return nil, orderApplicationError(err)
	}
	return value, nil
}

func (c Client) localPassQueue(ctx context.Context, owner, event, after string) (passbooking.BookingPage, error) {
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return passbooking.BookingPage{}, err
	}
	value, err := c.LocalRegistration.Service.Queue(ctx, actor, event, after)
	if err != nil {
		return passbooking.BookingPage{}, orderApplicationError(err)
	}
	return value, nil
}
