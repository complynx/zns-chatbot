package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const legacyOrderPrefix = "orders|"
const legacyOrderBindingKind = "legacy_order_binding"

type legacyOrderReceipt struct {
	Digest  string                       `json:"digest"`
	Binding orders.LegacyCallbackBinding `json:"binding"`
	Code    string                       `json:"code,omitempty"`
}

// Only host-owned bot.interactions is accessed here. Core source identities and
// authorization are resolved through the authenticated API, then pinned before effects.
func (b *Bot) bindLegacyOrder(ctx context.Context, in incoming, update int64) (legacyOrderReceipt, error) {
	digest := sha256.Sum256([]byte(in.text))
	hash := hex.EncodeToString(digest[:])
	var receipt legacyOrderReceipt
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions
	WHERE owner=$1 AND update_id=$2 AND kind=$3`, in.owner, update, legacyOrderBindingKind).Scan(&receipt)
	if errors.Is(err, pgx.ErrNoRows) {
		receipt.Digest = hash
		receipt.Binding, err = b.API.ResolveLegacyOrderCallback(ctx, in.owner,
			orders.LegacyCallbackRequest{Data: in.text, Event: b.currentOrderEvent()})
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); ok &&
			problem.Status < http.StatusInternalServerError {
			receipt.Code, err = problem.Code, nil
		}
		if err != nil {
			return receipt, err
		}
		receipt.Binding.Command.Key = fmt.Sprintf("tg-legacy-order-%d", update)
		if err = b.record(ctx, in.owner, update, legacyOrderBindingKind, receipt); err != nil {
			return receipt, err
		}
		err = b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions
		WHERE owner=$1 AND update_id=$2 AND kind=$3`, in.owner, update, legacyOrderBindingKind).Scan(&receipt)
	}
	if err == nil && receipt.Digest != hash {
		receipt.Code = "invalid_callback"
	}
	return receipt, err
}

func (b *Bot) handleLegacyOrder(ctx context.Context, in incoming, update telegram.Update) error {
	receipt, err := b.bindLegacyOrder(ctx, in, update.ID)
	if err != nil {
		return err
	}
	notice, err := b.legacyOrderAction(ctx, in, update.ID, receipt)
	if err != nil {
		return err
	}
	if err = b.record(ctx, in.owner, update.ID, "legacy_order_reply", notice); err != nil {
		return err
	}
	if _, err = b.editOrSend(ctx, telegram.Send{ChatID: in.chat, MessageID: update.Callback.Message.ID,
		Text: notice, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}); err != nil {
		return err
	}
	if receipt.Code == "" && receipt.Binding.Action != "close" {
		if err = b.renderLegacyOrders(ctx, in); err != nil {
			return err
		}
	}
	b.acknowledge(ctx, update.Callback.ID)
	return nil
}

func (b *Bot) renderLegacyOrders(ctx context.Context, in incoming) error {
	_, err := b.API.ResolveLegacyOrderCallback(ctx, in.owner,
		orders.LegacyCallbackRequest{Data: "orders|start", Event: b.currentOrderEvent()})
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	if err != nil {
		return err
	}
	return b.RenderOrders(ctx, in.owner, in.chat)
}

func (b *Bot) legacyOrderAction(
	ctx context.Context,
	in incoming,
	update int64,
	receipt legacyOrderReceipt,
) (string, error) {
	if receipt.Code != "" {
		return b.orderMessage(ctx, in.owner, i18n.OrderRejected, map[string]string{orderCodeParameter: receipt.Code})
	}
	binding := receipt.Binding
	if binding.Command.Name != "" {
		return b.executeOrder(ctx, in.owner, update, binding.Command)
	}
	// Saved navigation bindings never grant lasting read permission. Refresh
	// access through Core without replacing the pinned event/order or version.
	current, err := b.API.ResolveLegacyOrderCallback(ctx, in.owner,
		orders.LegacyCallbackRequest{Data: in.text, Event: binding.EventID})
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return b.orderMessage(ctx, in.owner, i18n.OrderRejected, map[string]string{orderCodeParameter: problem.Code})
	}
	if err != nil {
		return "", err
	}
	if current.EventID != binding.EventID || current.OrderID != binding.OrderID {
		return b.orderMessage(ctx, in.owner, i18n.OrderUnavailable, nil)
	}
	switch binding.Action {
	case "pay":
		return b.showPaymentInstructions(ctx, in, binding.OrderID)
	case "xlsx":
		return b.exportOrders(ctx, in, update)
	case "close":
		return b.orderMessage(ctx, in.owner, i18n.OrderClosed, nil)
	default:
		return b.orderMessage(ctx, in.owner, i18n.OrdersHint, nil)
	}
}
