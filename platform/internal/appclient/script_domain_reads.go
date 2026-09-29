package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassEventsPage(
	ctx context.Context,
	owner, cursor string,
) (core.ReadPage[passbooking.NavigationEvent], error) {
	if c.LocalRegistration != nil {
		return c.localPassEventsPage(ctx, owner, cursor)
	}
	var result core.ReadPage[passbooking.NavigationEvent]
	err := c.Call(ctx, owner, http.MethodGet, "/v1/passes/events/page?cursor="+url.QueryEscape(cursor), nil, &result)
	return registrationHTTPResult(result, ReadError(err))
}

func (c Client) PassEventDetail(ctx context.Context, owner, event, cursor string) (core.ReadChunk, error) {
	if c.LocalRegistration != nil {
		return c.localPassEventDetail(ctx, owner, event, cursor)
	}
	var result core.ReadChunk
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/detail?cursor="+url.QueryEscape(cursor),
		nil,
		&result,
	)
	return registrationHTTPResult(result, ReadError(err))
}

func (c Client) MassageSlotsPage(
	ctx context.Context,
	owner, event, party, cursor string, length int,
) (core.ReadPage[massage.NavigationSlot], error) {
	var result core.ReadPage[massage.NavigationSlot]
	query := url.Values{
		knowledgeEventQuery: {event},
		massagePartyQuery:   {party},
		"length":            {strconv.Itoa(length)},
		memoryCursorQuery:   {cursor},
	}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/slots/page?"+query.Encode(), nil, &result)
	return result, ReadError(err)
}

func (c Client) MassageProviderDetail(
	ctx context.Context,
	owner, event, provider, cursor string,
) (core.ReadChunk, error) {
	var result core.ReadChunk
	query := url.Values{knowledgeEventQuery: {event}, memoryCursorQuery: {cursor}}
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/massage/providers/"+url.PathEscape(provider)+"/detail?"+query.Encode(),
		nil,
		&result,
	)
	return result, ReadError(err)
}
