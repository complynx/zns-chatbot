package appclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) ExportOrders(ctx context.Context, owner, event string) ([]byte, error) {
	if c.LocalOrders != nil {
		return directOrder(
			ctx,
			c,
			owner,
			func(s orders.Service, actor string) ([]byte, error) { return s.Export(ctx, actor, event) },
		)
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.Base+"/v1/order-events/"+url.PathEscape(event)+"/export",
		http.NoBody,
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTPClient()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("core API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, coreResponseError(ctx, response, "invalid export response")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, orders.MaxExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > orders.MaxExportBytes {
		return nil, errors.New("invalid export size")
	}
	return body, nil
}
