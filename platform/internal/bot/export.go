package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const actionExport = "export"

func (c APIClient) ExportOrders(ctx context.Context, owner, event string) ([]byte, error) {
	token, err := c.userToken(ctx, owner)
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
	client := c.httpClient()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("core API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, maxAPIBytes)).Decode(&problem); err != nil {
			return nil, err
		}
		problem.Status = response.StatusCode
		return nil, &problem
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

func (b *Bot) exportOrders(ctx context.Context, in incoming, update int64) (string, error) {
	if err := b.rememberOrderLocale(ctx, in.owner, update); err != nil {
		return "", err
	}
	var sent bool
	err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='order_export')`, in.owner, update).
		Scan(&sent)
	if err != nil {
		return "", err
	}
	if sent {
		return b.orderMessage(ctx, in.owner, i18n.OrderExported, nil)
	}
	body, err := b.API.ExportOrders(ctx, in.owner, b.currentOrderEvent())
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return b.orderMessage(
			ctx,
			in.owner,
			i18n.OrderExportUnavailable,
			map[string]string{orderCodeParameter: problem.Code},
		)
	}
	if err != nil {
		return "", err
	}
	message, err := b.TG.SendDocument(ctx, in.chat, "orders.xlsx", body)
	if err != nil {
		return "", err
	}
	err = b.record(ctx, in.owner, update, "order_export", map[string]any{
		"origin": in.origin, "event_id": b.currentOrderEvent(), "filename": "orders.xlsx", "message_id": message.ID,
	})
	if err != nil {
		return "", err
	}
	return b.orderMessage(ctx, in.owner, i18n.OrderExported, nil)
}
