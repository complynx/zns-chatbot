package appclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) OrderEvent(ctx context.Context, owner, event string) (orders.Event, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, _ string) (orders.Event, error) { return s.Event(ctx, event) },
		)
	}
	var result orders.Event
	err := c.Call(ctx, owner, http.MethodGet, "/v1/order-events/"+url.PathEscape(event), nil, &result)
	if _, limited := errors.AsType[*apiResponseLimitError](err); limited {
		return c.orderEventTransport(ctx, owner, event)
	}
	return result, err
}

func (c Client) Orders(ctx context.Context, owner, event string) ([]orders.Order, error) {
	if c.LocalOrders != nil {
		return c.localOrderPages(ctx, owner, event, false)
	}
	return c.orderPages(ctx, owner, "/v1/order-events/"+url.PathEscape(event)+"/orders")
}

func (c Client) Order(ctx context.Context, owner, event, id string) (orders.Order, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) (orders.Order, error) { return s.Get(ctx, actor, event, id) },
		)
	}
	var result orders.Order
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/orders/"+url.PathEscape(id),
		nil,
		&result,
	)
	return result, err
}

func (c Client) QuoteOrder(
	ctx context.Context,
	owner, event string,
	choice orders.ChoiceInput,
) (orders.Choice, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) (orders.Choice, error) { return s.Quote(ctx, actor, event, choice) },
		)
	}
	var result orders.Choice
	err := c.Call(ctx, owner, http.MethodPost, "/v1/order-events/"+url.PathEscape(event)+"/quote", choice, &result)
	return result, err
}

func (c Client) ExecuteOrder(ctx context.Context, owner string, command orders.Command) (orders.Order, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) (orders.Order, error) { return s.Execute(ctx, actor, command) },
		)
	}
	var result orders.Order
	err := c.Call(ctx, owner, http.MethodPost, "/v1/order-actions", command, &result)
	return result, err
}

func (c Client) PaymentAdmins(ctx context.Context, owner, event string) ([]orders.PaymentAdmin, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, _ string) ([]orders.PaymentAdmin, error) {
			return s.PaymentAdmins(ctx, event)
		})
	}
	var result []orders.PaymentAdmin
	err := c.Call(ctx, owner, http.MethodGet, "/v1/order-events/"+url.PathEscape(event)+"/admins", nil, &result)
	return result, err
}

func (c Client) PaymentInstructions(
	ctx context.Context,
	owner, event, id string,
) (orders.PaymentInstructions, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.PaymentInstructions, error) {
			return s.Instructions(ctx, actor, event, id)
		})
	}
	var result orders.PaymentInstructions
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/orders/"+url.PathEscape(id)+"/payment-instructions",
		nil,
		&result,
	)
	return result, err
}

func (c Client) PaymentInbox(ctx context.Context, owner, event string) ([]orders.Order, error) {
	if c.LocalOrders != nil {
		return c.localOrderPages(ctx, owner, event, true)
	}
	return c.orderPages(ctx, owner, "/v1/order-events/"+url.PathEscape(event)+"/payment-inbox")
}

func (c Client) orderPages(ctx context.Context, owner, path string) ([]orders.Order, error) {
	result := []orders.Order{}
	cursor := ""
	for {
		var page orders.Page
		if err := c.Call(ctx, owner, http.MethodGet, path+"?cursor="+url.QueryEscape(cursor), nil, &page); err != nil {
			return nil, err
		}
		result = append(result, page.Orders...)
		if page.Next == "" {
			return result, nil
		}
		cursor = page.Next
	}
}

func (c Client) OrderHistory(ctx context.Context, owner, event string) ([]orders.Change, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) ([]orders.Change, error) { return s.History(ctx, actor, event) },
		)
	}
	path := "/v1/order-events/" + url.PathEscape(event)
	var recent []orders.Change
	readErr := c.Call(ctx, owner, http.MethodGet, path+"/history", nil, &recent)
	if _, exceeded := errors.AsType[*apiResponseLimitError](readErr); !exceeded {
		return recent, readErr
	}
	var items []orders.HistoryItem
	if err := c.Call(ctx, owner, http.MethodGet, path+"/history-recent", nil, &items); err != nil {
		return nil, err
	}
	result := make([]orders.Change, 0, len(items))
	for _, item := range items {
		change, err := c.orderHistoryChange(ctx, owner, path, item)
		if err != nil {
			return nil, err
		}
		result = append(result, change)
	}
	return result, nil
}

func (c Client) UploadProof(ctx context.Context, owner, filename string, body []byte) (orders.Proof, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.Proof, error) {
			return s.UploadProof(ctx, actor, filename, body)
		})
	}
	var result orders.Proof
	err := c.Request(ctx, owner, http.MethodPost, "/v1/order-proofs?filename="+url.QueryEscape(filename), body, &result)
	return result, err
}

func (c Client) OrderProof(ctx context.Context, owner, event, id string) (orders.Proof, error) {
	if c.LocalOrders != nil {
		return directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.Proof, error) {
			return s.OrderProofMetadata(ctx, actor, event, id)
		})
	}
	var result orders.Proof
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/orders/"+url.PathEscape(id)+"/proof",
		nil,
		&result,
	)
	return result, err
}

// OrderByID resolves an existing order without rebinding it to the current event.
func (c Client) OrderByID(ctx context.Context, owner, id string) (orders.Order, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) (orders.Order, error) { return s.GetByID(ctx, actor, id) },
		)
	}
	var result orders.Order
	err := c.Call(ctx, owner, http.MethodGet, "/v1/orders/"+url.PathEscape(id), nil, &result)
	return result, err
}
