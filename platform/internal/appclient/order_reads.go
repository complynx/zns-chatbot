package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) OrdersPage(ctx context.Context, owner, event, cursor string, inbox bool) (orders.Page, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.Page, error) {
			return s.ListPage(ctx, actor, event, cursor, inbox)
		})
	}
	resource := "/orders"
	if inbox {
		resource = "/payment-inbox"
	}
	var result orders.Page
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+resource+"?cursor="+url.QueryEscape(cursor),
		nil,
		&result,
	)
	return result, err
}
func (c Client) OrderEventsPage(ctx context.Context, owner, cursor string) (core.ReadPage[string], error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (core.ReadPage[string], error) {
			return s.EventsPage(ctx, actor, cursor)
		})
	}
	var result core.ReadPage[string]
	err := c.Call(ctx, owner, http.MethodGet, "/v1/order-events?cursor="+url.QueryEscape(cursor), nil, &result)
	return result, err
}

func (c Client) OrderHistoryPage(
	ctx context.Context,
	owner, event, cursor string,
) (core.ReadPage[orders.HistoryItem], error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) (core.ReadPage[orders.HistoryItem], error) {
				return s.HistoryPage(ctx, actor, event, cursor)
			},
		)
	}
	var result core.ReadPage[orders.HistoryItem]
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/history-page?cursor="+url.QueryEscape(cursor),
		nil,
		&result,
	)
	return result, err
}
func (c Client) OrderHistoryDetail(ctx context.Context, owner, event, entry, cursor string) (core.ReadChunk, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (core.ReadChunk, error) {
			return s.HistoryDetail(ctx, actor, event, entry, cursor)
		})
	}
	var result core.ReadChunk
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/history/"+url.PathEscape(entry)+"?cursor="+url.QueryEscape(cursor),
		nil,
		&result,
	)
	return result, err
}
func (c Client) ReviewOrder(ctx context.Context, owner, event, id string) (orders.Order, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.Order, error) {
			return s.ReviewOrder(ctx, actor, event, id)
		})
	}
	var result orders.Order
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/review/"+url.PathEscape(id),
		nil,
		&result,
	)
	return result, err
}
