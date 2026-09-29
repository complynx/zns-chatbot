package orders

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/complynx/zns-chatbot/platform/internal/orders/dbgen"

	"github.com/jackc/pgx/v5"
)

const notificationPageSize = 25

type Notification struct {
	DeliveryAttempt int64  `json:"delivery_attempt"`
	MessageID       int64  `json:"message_id,omitempty"`
	DeliveryText    string `json:"delivery_text,omitempty"`
	FollowupPending bool   `json:"followup_pending,omitempty"`

	ID         int64    `json:"id"`
	Recipient  string   `json:"recipient"`
	TelegramID int64    `json:"telegram_id"`
	EventID    string   `json:"event_id"`
	OrderID    string   `json:"order_id"`
	Kind       string   `json:"kind"`
	Version    int64    `json:"version"`
	Attempt    string   `json:"attempt,omitempty"`
	State      string   `json:"state"`
	Total      Money    `json:"total"`
	Country    string   `json:"country,omitempty"`
	Removed    []string `json:"removed,omitempty"`
	Current    bool     `json:"current"`
}

func enqueueNotification(
	ctx context.Context,
	tx pgx.Tx,
	botID int64,
	pending *[]delivery.Registration,
	recipient, kind string,
	order Order,
	removed []string,
) error {
	notice := Notification{Recipient: recipient, EventID: order.EventID, OrderID: order.ID, Kind: kind,
		Version: order.Version, Attempt: order.Attempt, State: order.State, Total: order.Choice.Total,
		Country: order.Country, Removed: removed}
	payload, err := json.Marshal(notice)
	if err != nil {
		return err
	}
	row, err := dbgen.New(tx).EnqueueNotification(ctx, dbgen.EnqueueNotificationParams{
		Recipient: recipient, OrderID: order.ID, Payload: payload, BotID: botID})
	if err != nil {
		return err
	}
	return collectNotificationRegistration(ctx, tx, botID, row.ID, row.DeliveryChat, pending)
}

func (op *operation) notifyPayment(ctx context.Context, order Order) error {
	switch op.command.Name {
	case stateCash, actionCountry:
		return enqueueNotification(
			ctx,
			op.tx,
			op.deliveryBotID,
			&op.notificationRegistrations,
			order.PaymentAdmin,
			"payment_request",
			order,
			nil,
		)
	case actionAccept, actionReject:
		return enqueueNotification(
			ctx,
			op.tx,
			op.deliveryBotID,
			&op.notificationRegistrations,
			order.Owner,
			op.command.Name,
			order,
			nil,
		)
	}
	return nil
}
