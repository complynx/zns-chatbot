package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const refundListAction = "refund_list"
const refundConfirmAction = "refund_confirm"
const refundCardPrefix = "refund:"
const ownerField = "owner"

func (b *Bot) handleRefundCommand(
	ctx context.Context,
	in incoming,
	update int64,
	command orders.Command,
) (string, error) {
	if command.EventID != b.currentOrderEvent() {
		return b.orderMessage(ctx, in.owner, i18n.OrderUnavailable, nil)
	}
	if command.Name == refundListAction {
		if command.Version < 0 {
			return b.orderMessage(ctx, in.owner, i18n.OrderUnavailable, nil)
		}
		if err := b.record(ctx, in.owner, update, "refund_page:"+command.EventID, command.Version); err != nil {
			return "", err
		}
		return b.orderMessage(ctx, in.owner, i18n.RefundList, nil)
	}
	id, err := strconv.ParseInt(command.OrderID, 10, 64)
	if err != nil || id <= 0 {
		return b.orderMessage(ctx, in.owner, i18n.OrderUnavailable, nil)
	}
	_, err = b.API.ConfirmRefund(ctx, in.owner, orders.RefundConfirmation{
		ID: id, Version: command.Version, Key: fmt.Sprintf("tg-refund-%d", update), Confirmed: true,
	})
	if core.IsDatabaseFailure(err) {
		return "", err
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok {
		return b.orderMessage(ctx, in.owner, i18n.OrderRejected, map[string]string{orderCodeParameter: problem.Code})
	}
	if err != nil {
		return "", err
	}
	return b.orderMessage(ctx, in.owner, i18n.RefundCompleted, nil)
}

func (b *Bot) refundCursor(ctx context.Context, owner string) (int64, error) {
	var raw []byte
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND kind=$2 ORDER BY update_id DESC LIMIT 1`,
		owner, "refund_page:"+b.currentOrderEvent()).
		Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	var before int64
	err = json.Unmarshal(raw, &before)
	return before, err
}
