package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PrivilegedReadCapabilities(
	ctx context.Context,
	owner string,
) (core.PrivilegedReadCapabilities, error) {
	var value core.PrivilegedReadCapabilities
	err := c.Call(ctx, owner, http.MethodGet, "/v1/privileged-read-capabilities", nil, &value)
	return value, err
}

func (c Client) PrivilegedReadEvents(
	ctx context.Context,
	owner, cursor string,
) (core.ReadPage[core.PrivilegedReadEvent], error) {
	var value core.ReadPage[core.PrivilegedReadEvent]
	err := c.Call(ctx, owner, http.MethodGet, "/v1/privileged-read-events?cursor="+url.QueryEscape(cursor), nil, &value)
	return value, ReadError(err)
}

func (c Client) PassPaymentHistory(
	ctx context.Context,
	owner, event, cursor string,
) (core.ReadPage[passbooking.PaymentHistoryEntry], error) {
	if c.LocalRegistration != nil {
		return c.localPassPaymentHistory(ctx, owner, event, cursor)
	}
	var value core.ReadPage[passbooking.PaymentHistoryEntry]
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/payment-history?cursor="+url.QueryEscape(cursor),
		nil,
		&value,
	)
	return registrationHTTPResult(value, ReadError(err))
}

func (c Client) PractitionerSchedule(
	ctx context.Context,
	owner, event, cursor string,
) (core.ReadPage[massage.PractitionerWork], error) {
	var value core.ReadPage[massage.PractitionerWork]
	query := url.Values{knowledgeEventQuery: {event}, memoryCursorQuery: {cursor}}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/practitioner/schedule?"+query.Encode(), nil, &value)
	return value, ReadError(err)
}

func (c Client) PractitionerBookings(
	ctx context.Context,
	owner, event, party, cursor string,
) (core.ReadPage[massage.Reservation], error) {
	var value core.ReadPage[massage.Reservation]
	query := url.Values{
		knowledgeEventQuery: {event},
		massagePartyQuery:   {party},
		memoryCursorQuery:   {cursor},
	}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/practitioner/bookings?"+query.Encode(), nil, &value)
	return value, ReadError(err)
}
