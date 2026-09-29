package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c APIClient) PassEvents(ctx context.Context, actor string) ([]passbooking.Event, error) {
	var value []passbooking.Event
	err := c.call(ctx, actor, http.MethodGet, "/v1/passes/events", nil, &value)
	return value, err
}

func (c APIClient) PassBooking(ctx context.Context, actor, event string) (passbooking.Booking, error) {
	var value passbooking.Booking
	err := c.call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/me", nil, &value)
	return value, err
}

func (c APIClient) PassInvitations(
	ctx context.Context,
	actor, event, after string,
) (passbooking.InvitationPage, error) {
	var value passbooking.InvitationPage
	err := c.call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/invitations?"+url.Values{passAfterQuery: {after}}.Encode(),
		nil,
		&value,
	)
	return value, err
}

func (c APIClient) PassPaymentAdmins(ctx context.Context, actor, event string) ([]passbooking.Contact, error) {
	var value []passbooking.Contact
	err := c.call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/payment-admins", nil, &value)
	return value, err
}

func (c APIClient) ExecutePassBooking(
	ctx context.Context, actor string, command passbooking.Command,
) (passbooking.Booking, error) {
	var value passbooking.Booking
	err := c.call(ctx, actor, http.MethodPost, "/v1/passes/actions", command, &value)
	return value, err
}

func (c APIClient) PassQueue(ctx context.Context, actor, event, after string) (passbooking.BookingPage, error) {
	var value passbooking.BookingPage
	err := c.call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/queue?"+url.Values{passAfterQuery: {after}}.Encode(),
		nil,
		&value,
	)
	return value, err
}

const passAfterQuery = "after"
