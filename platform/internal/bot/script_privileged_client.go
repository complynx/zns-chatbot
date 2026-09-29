package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c APIClient) privilegedReadCapabilities(
	ctx context.Context,
	owner string,
) (core.PrivilegedReadCapabilities, error) {
	var value core.PrivilegedReadCapabilities
	err := c.call(ctx, owner, http.MethodGet, "/v1/privileged-read-capabilities", nil, &value)
	return value, err
}

func (c APIClient) privilegedReadEvents(
	ctx context.Context,
	owner, cursor string,
) (core.ReadPage[core.PrivilegedReadEvent], error) {
	var value core.ReadPage[core.PrivilegedReadEvent]
	err := c.call(ctx, owner, http.MethodGet, "/v1/privileged-read-events?cursor="+url.QueryEscape(cursor), nil, &value)
	return value, scriptDomainAPIError(err)
}

func (c APIClient) passPaymentHistory(
	ctx context.Context,
	owner, event, cursor string,
) (core.ReadPage[passbooking.PaymentHistoryEntry], error) {
	var value core.ReadPage[passbooking.PaymentHistoryEntry]
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/payment-history?cursor="+url.QueryEscape(cursor),
		nil,
		&value,
	)
	return value, scriptDomainAPIError(err)
}

func (c APIClient) practitionerSchedule(
	ctx context.Context,
	owner, event, cursor string,
) (core.ReadPage[massage.PractitionerWork], error) {
	var value core.ReadPage[massage.PractitionerWork]
	query := url.Values{knowledgeEventQuery: {event}, memoryCursorQuery: {cursor}}
	err := c.call(ctx, owner, http.MethodGet, "/v1/massage/practitioner/schedule?"+query.Encode(), nil, &value)
	return value, scriptDomainAPIError(err)
}

func (c APIClient) practitionerBookings(
	ctx context.Context,
	owner string,
	args scriptPrivilegedArguments,
) (core.ReadPage[massage.Reservation], error) {
	var value core.ReadPage[massage.Reservation]
	query := url.Values{
		knowledgeEventQuery: {args.Event},
		massagePartyQuery:   {args.Party},
		memoryCursorQuery:   {args.Cursor},
	}
	err := c.call(ctx, owner, http.MethodGet, "/v1/massage/practitioner/bookings?"+query.Encode(), nil, &value)
	return value, scriptDomainAPIError(err)
}
