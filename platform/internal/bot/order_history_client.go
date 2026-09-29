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

// Read one immutable audit snapshot through the existing bounded detail route.
// Each request rechecks the current principal and owner; cursors grant no access.
func (c APIClient) orderHistoryChange(
	ctx context.Context,
	owner, path string,
	item orders.HistoryItem,
) (orders.Change, error) {
	var body strings.Builder
	cursor := ""
	for {
		var chunk core.ReadChunk
		err := c.call(ctx, owner, http.MethodGet,
			path+"/history-recent/"+url.PathEscape(item.ID)+"?cursor="+url.QueryEscape(cursor), nil, &chunk)
		if err != nil {
			return orders.Change{}, err
		}
		body.WriteString(chunk.JSON)
		if !chunk.More {
			break
		}
		if chunk.NextCursor == "" || chunk.NextCursor == cursor {
			return orders.Change{}, errors.New("order history cursor did not advance")
		}
		cursor = chunk.NextCursor
	}
	var detail struct {
		orders.HistoryItem

		Change *orders.Change `json:"change"`
	}
	if err := json.Unmarshal([]byte(body.String()), &detail); err != nil {
		return orders.Change{}, err
	}
	if detail.HistoryItem != item || detail.Change == nil {
		return orders.Change{}, errors.New("order history snapshot changed")
	}
	return *detail.Change, nil
}
