package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c APIClient) orderEventChunk(
	ctx context.Context,
	owner, event, raw string,
	transport bool,
) (core.ReadChunk, error) {
	route := "/catalog-read"
	if transport {
		route = "/catalog-transport"
	}
	var result core.ReadChunk
	err := c.call(ctx, owner, http.MethodGet,
		"/v1/order-events/"+url.PathEscape(event)+route+"?cursor="+url.QueryEscape(raw), nil, &result)
	return result, err
}

// Each page rechecks current access and the catalog digest. A changing catalog
// fails with read_stale instead of combining different menu or price versions.
func (c APIClient) orderEventTransport(ctx context.Context, owner, event string) (orders.Event, error) {
	var body strings.Builder
	cursor := ""
	for {
		chunk, err := c.orderEventChunk(ctx, owner, event, cursor, true)
		if err != nil {
			return orders.Event{}, err
		}
		body.WriteString(chunk.JSON)
		if !chunk.More {
			break
		}
		if chunk.NextCursor == "" || chunk.NextCursor == cursor {
			return orders.Event{}, errors.New("order catalog cursor did not advance")
		}
		cursor = chunk.NextCursor
	}
	var result orders.Event
	if err := json.Unmarshal([]byte(body.String()), &result); err != nil {
		return result, err
	}
	if result.ID != event {
		return orders.Event{}, errors.New("order catalog event changed")
	}
	return result, nil
}
