package legacyfood

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood/dbgen"
)

type Notification struct {
	DeliveryAttempt int64                     `json:"delivery_attempt"`
	MessageID       int64                     `json:"message_id,omitempty"`
	Wire            *notificationwire.Payload `json:"wire,omitempty"`
	DeliveryText    string                    `json:"delivery_text,omitempty"`
	FollowupPending bool                      `json:"followup_pending,omitempty"`

	Current    bool            `json:"current"`
	ID         int64           `json:"id"`
	EventID    string          `json:"event_id"`
	Owner      string          `json:"owner"`
	TelegramID int64           `json:"telegram_id"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
}

// QueueReminders preserves the domain windows and binds trusted delivery identity.
// Each direct SQL operation returns a safe database failure; cancellation and
// delivery domain errors keep their own identity.
func (s Service) QueueReminders(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := dbgen.New(tx)
	rows, err := q.QueueOrderReminders(
		ctx,
		dbgen.QueueOrderRemindersParams{BotID: s.Delivery.BotID, EventBotID: s.BotID},
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	missing, err := q.QueueMissingOrderReminders(
		ctx,
		dbgen.QueueMissingOrderRemindersParams{BotID: s.Delivery.BotID, EventBotID: s.BotID},
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	var pending []delivery.Registration
	for _, row := range append(rows, missing...) {
		if err = collectNotificationRegistration(
			ctx,
			tx,
			s.Delivery.BotID,
			row.ID,
			row.DeliveryChat,
			&pending,
		); err != nil {
			return err
		}
	}
	if err = delivery.RegisterBatch(ctx, tx, s.Delivery.BotID, pending); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
