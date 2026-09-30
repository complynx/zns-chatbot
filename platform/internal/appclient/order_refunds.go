package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) RefundTasks(ctx context.Context, owner, event string, before int64) (orders.RefundPage, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.RefundPage, error) {
			return s.RefundTasks(ctx, actor, event, before)
		})
	}
	var result orders.RefundPage
	err := c.Call(ctx, owner, http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/refunds?before="+strconv.FormatInt(before, 10), nil, &result)
	return result, err
}

func (c Client) Refund(ctx context.Context, owner string, id int64) (orders.RefundTask, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.RefundTask, error) {
			return s.Refund(ctx, actor, id)
		})
	}
	var result orders.RefundTask
	err := c.Call(ctx, owner, http.MethodGet, "/v1/order-refunds/"+strconv.FormatInt(id, 10), nil, &result)
	return result, err
}

func (c Client) ConfirmRefund(
	ctx context.Context,
	owner string,
	command orders.RefundConfirmation,
) (orders.RefundTask, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.RefundTask, error) {
			return s.ConfirmRefund(ctx, actor, command)
		})
	}
	var result orders.RefundTask
	err := c.Call(ctx, owner, http.MethodPost, "/v1/order-refunds/confirm", command, &result)
	return result, err
}
