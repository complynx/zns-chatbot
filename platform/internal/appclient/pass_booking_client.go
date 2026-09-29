package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassEvents(ctx context.Context, actor string) ([]passbooking.Event, error) {
	if c.LocalRegistration != nil {
		return c.localPassEvents(ctx, actor)
	}
	var value []passbooking.Event
	err := c.Call(ctx, actor, http.MethodGet, "/v1/passes/events", nil, &value)
	return registrationHTTPResult(value, err)
}

func (c Client) PassBooking(ctx context.Context, actor, event string) (passbooking.Booking, error) {
	if c.LocalRegistration != nil {
		return c.localPassBooking(ctx, actor, event)
	}
	var value passbooking.Booking
	err := c.Call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/me", nil, &value)
	return registrationHTTPResult(value, err)
}

func (c Client) PassInvitations(
	ctx context.Context,
	actor, event, after string,
) (passbooking.InvitationPage, error) {
	if c.LocalRegistration != nil {
		return c.localPassInvitations(ctx, actor, event, after)
	}
	var value passbooking.InvitationPage
	err := c.Call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/invitations?"+url.Values{passAfterQuery: {after}}.Encode(),
		nil,
		&value,
	)
	return registrationHTTPResult(value, err)
}

func (c Client) PassPaymentAdmins(ctx context.Context, actor, event string) ([]passbooking.Contact, error) {
	if c.LocalRegistration != nil {
		return c.localPassPaymentAdmins(ctx, actor, event)
	}
	var value []passbooking.Contact
	err := c.Call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/payment-admins", nil, &value)
	return registrationHTTPResult(value, err)
}

func (c Client) ExecutePassBooking(
	ctx context.Context, actor string, command passbooking.Command,
) (passbooking.Booking, error) {
	if c.LocalRegistration != nil {
		return c.localExecutePassBooking(ctx, actor, command)
	}
	var value passbooking.Booking
	err := c.Call(ctx, actor, http.MethodPost, "/v1/passes/actions", command, &value)
	return registrationHTTPResult(value, err)
}

func (c Client) PassQueue(ctx context.Context, actor, event, after string) (passbooking.BookingPage, error) {
	if c.LocalRegistration != nil {
		return c.localPassQueue(ctx, actor, event, after)
	}
	var value passbooking.BookingPage
	err := c.Call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/queue?"+url.Values{passAfterQuery: {after}}.Encode(),
		nil,
		&value,
	)
	return registrationHTTPResult(value, err)
}
