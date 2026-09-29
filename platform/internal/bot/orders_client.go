package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c APIClient) OrderEvent(ctx context.Context, owner, event string) (orders.Event, error) {
	var result orders.Event
	err := c.call(ctx, owner, http.MethodGet, "/v1/order-events/"+url.PathEscape(event), nil, &result)
	if _, limited := errors.AsType[*apiResponseLimitError](err); limited {
		return c.orderEventTransport(ctx, owner, event)
	}
	return result, err
}

func (c APIClient) Orders(ctx context.Context, owner, event string) ([]orders.Order, error) {
	return c.orderPages(ctx, owner, "/v1/order-events/"+url.PathEscape(event)+"/orders")
}

func (c APIClient) Order(ctx context.Context, owner, event, id string) (orders.Order, error) {
	var result orders.Order
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/orders/"+url.PathEscape(id),
		nil,
		&result,
	)
	return result, err
}

func (c APIClient) QuoteOrder(
	ctx context.Context,
	owner, event string,
	choice orders.ChoiceInput,
) (orders.Choice, error) {
	var result orders.Choice
	err := c.call(ctx, owner, http.MethodPost, "/v1/order-events/"+url.PathEscape(event)+"/quote", choice, &result)
	return result, err
}

func (c APIClient) ExecuteOrder(ctx context.Context, owner string, command orders.Command) (orders.Order, error) {
	var result orders.Order
	err := c.call(ctx, owner, http.MethodPost, "/v1/order-actions", command, &result)
	return result, err
}

func (c APIClient) PaymentAdmins(ctx context.Context, owner, event string) ([]orders.PaymentAdmin, error) {
	var result []orders.PaymentAdmin
	err := c.call(ctx, owner, http.MethodGet, "/v1/order-events/"+url.PathEscape(event)+"/admins", nil, &result)
	return result, err
}

func (c APIClient) PaymentInstructions(
	ctx context.Context,
	owner, event, id string,
) (orders.PaymentInstructions, error) {
	var result orders.PaymentInstructions
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+"/orders/"+url.PathEscape(id)+"/payment-instructions",
		nil,
		&result,
	)
	return result, err
}

func (c APIClient) PaymentInbox(ctx context.Context, owner, event string) ([]orders.Order, error) {
	return c.orderPages(ctx, owner, "/v1/order-events/"+url.PathEscape(event)+"/payment-inbox")
}

func (c APIClient) orderPages(ctx context.Context, owner, path string) ([]orders.Order, error) {
	result := []orders.Order{}
	cursor := ""
	for {
		var page orders.Page
		if err := c.call(ctx, owner, http.MethodGet, path+"?cursor="+url.QueryEscape(cursor), nil, &page); err != nil {
			return nil, err
		}
		result = append(result, page.Orders...)
		if page.Next == "" {
			return result, nil
		}
		cursor = page.Next
	}
}

func (c APIClient) OrderHistory(ctx context.Context, owner, event string) ([]orders.Change, error) {
	path := "/v1/order-events/" + url.PathEscape(event)
	var recent []orders.Change
	readErr := c.call(ctx, owner, http.MethodGet, path+"/history", nil, &recent)
	if _, exceeded := errors.AsType[*apiResponseLimitError](readErr); !exceeded {
		return recent, readErr
	}
	var items []orders.HistoryItem
	if err := c.call(ctx, owner, http.MethodGet, path+"/history-recent", nil, &items); err != nil {
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

func (c APIClient) UploadProof(ctx context.Context, owner, filename string, body []byte) (orders.Proof, error) {
	var result orders.Proof
	err := c.request(ctx, owner, http.MethodPost, "/v1/order-proofs?filename="+url.QueryEscape(filename), body, &result)
	return result, err
}

func (c APIClient) OrderProof(ctx context.Context, owner, event, id string) (orders.Proof, error) {
	var result orders.Proof
	err := c.call(
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
func (c APIClient) OrderByID(ctx context.Context, owner, id string) (orders.Order, error) {
	var result orders.Order
	err := c.call(ctx, owner, http.MethodGet, "/v1/orders/"+url.PathEscape(id), nil, &result)
	return result, err
}
