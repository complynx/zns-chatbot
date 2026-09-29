package appclient

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) OrderChoiceSnapshot(ctx context.Context, owner, event, id string) (orders.ChoiceSnapshot, error) {
	var result orders.ChoiceSnapshot
	var err error
	if c.LocalOrders != nil {
		result, err = directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.ChoiceSnapshot, error) {
			return s.ChoiceSnapshot(ctx, actor, event, id)
		})
	} else {
		err = c.Call(ctx, owner, http.MethodGet,
			"/v1/order-events/"+url.PathEscape(event)+"/choice-snapshot?order_id="+url.QueryEscape(id), nil, &result)
	}
	if err != nil {
		return orders.ChoiceSnapshot{}, err
	}
	if !validChoiceDigest(result.Catalog) || (id != "" && !validChoiceDigest(result.Order)) ||
		(id == "" && result.Order != "") {
		return orders.ChoiceSnapshot{}, errors.New("invalid choice snapshot")
	}
	return result, nil
}

func validChoiceDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(value) == 64 && len(decoded) == 32
}
