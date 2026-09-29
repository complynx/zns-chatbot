package bot

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// DeliverMassageNotifications runs under the poller's existing session lock.
// A crash before storing Telegram's accepted message ID can duplicate a send.
// Once stored, retry edits the known message before acknowledging.
func (b *Bot) DeliverMassageNotifications(ctx context.Context) error {
	recipients, err := b.API.MassageNoticeRecipients(ctx)
	if err != nil {
		return err
	}
	delivered := 0
	for _, recipient := range recipients {
		if delivered >= massageDeliveryBatch {
			return nil
		}
		notices, noticeErr := b.API.MassageDeliveryNotices(ctx, recipient.Owner)
		if noticeErr != nil {
			return noticeErr
		}
		for _, notice := range notices {
			if delivered >= massageDeliveryBatch {
				return nil
			}
			delivered++
			if noticeErr = b.deliverMassageNotice(
				ctx,
				recipient.Owner,
				recipient.TelegramID,
				notice,
			); noticeErr != nil {
				b.logger().WarnContext(ctx, "massage notification pending", "error", noticeErr)
				break
			}
		}
	}
	return nil
}

const massageDeliveryBatch = 10

func (b *Bot) deliverMassageNotice(
	ctx context.Context,
	owner string,
	chat int64,
	delivery massage.DeliveryNotice,
) error {
	ctx, authErr := b.API.notificationContext(ctx, owner, chat)
	if authErr != nil {
		return authErr
	}
	notice, reservation := delivery.Notice, delivery.Reservation
	client, specialist := delivery.Client, delivery.Specialist
	if reservation.CancelledAt != nil && notice.Kind != massageNoticeCancelled {
		return b.API.CompleteMassageNotice(ctx, owner, notice.ID)
	}
	if notice.Kind == "additional" && time.Now().After(reservation.Start) {
		return b.API.CompleteMassageNotice(ctx, owner, notice.ID)
	}
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	title := i18n.MassageReminder
	switch notice.Kind {
	case "booked":
		title = i18n.MassageNewBooking
	case massageNoticeCancelled:
		title = i18n.MassageCancelNotice
	}
	r := massageRenderer{language: preference.Language}
	text := r.text(
		title,
	) + "\n" + massageWhen(
		reservation.Start,
	) + " · " + r.reservationQuote(
		reservation,
	) + "\n" + massageName(
		specialist,
	) + " · " + massageName(
		client,
	)
	payload := telegram.Send{
		ChatID: chat,
		Text:   text,
		Markup: telegram.Markup{
			Rows: [][]telegram.Button{{{Text: r.text(i18n.MassageHome), Data: massagePrefix + "open"}}},
		},
	}
	err = b.DB.QueryRow(ctx, `SELECT message_id FROM bot.massage_deliveries WHERE notice_id=$1`, notice.ID).
		Scan(&payload.MessageID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	messageID, err := b.editOrSend(ctx, payload)
	if err != nil {
		return err
	}
	_, err = b.DB.Exec(ctx, `INSERT INTO bot.massage_deliveries(notice_id,message_id) VALUES($1,$2)
	ON CONFLICT(notice_id) DO UPDATE SET message_id=$2`, notice.ID, messageID)
	if err != nil {
		return err
	}
	var opened bool
	if err = b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.massage_views WHERE owner=$1)`, owner).
		Scan(&opened); err != nil {
		return err
	}
	if opened {
		if err = b.RenderMassage(ctx, owner, chat, ""); err != nil {
			return err
		}
	}
	return b.API.CompleteMassageNotice(ctx, owner, notice.ID)
}

func (b *Bot) deliverQueuedNotifications(ctx context.Context) {
	if err := b.DeliverFoodNotifications(ctx); err != nil {
		b.logger().WarnContext(ctx, "food notification queue unavailable", "error", err)
	}
	if err := b.DeliverAdminMessages(ctx); err != nil {
		b.logger().WarnContext(ctx, "administrator message queue unavailable")
	}
	if err := b.DeliverNotifications(ctx); err != nil {
		b.logger().WarnContext(ctx, "notification queue unavailable", "error", err)
	}
	if err := b.DeliverMassageNotifications(ctx); err != nil {
		b.logger().WarnContext(ctx, "massage notification queue unavailable", "error", err)
	}
	if err := b.DeliverPassNotifications(ctx); err != nil {
		b.logger().WarnContext(ctx, "pass notification queue unavailable", "error", err)
	}
}

func (r *massageRenderer) reservationQuote(reservation massage.Reservation) string {
	value, _ := i18n.Translate(r.language, i18n.MassageQuote, map[string]string{
		"minutes": strconv.Itoa(
			int(reservation.End.Sub(reservation.Start).Minutes()),
		),
		"price": strconv.Itoa(reservation.Price),
	})
	return value
}

func (b *Bot) reconcileMassageViews(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.massage_views ORDER BY owner`)
	if err != nil {
		return err
	}
	type view struct {
		owner string
		chat  int64
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (view, error) {
		var result view
		scanErr := row.Scan(&result.owner, &result.chat)
		return result, scanErr
	})
	if err != nil {
		return err
	}
	for _, view := range views {
		viewContext, authErr := b.API.notificationContext(ctx, view.owner, view.chat)
		if authErr != nil {
			b.logger().WarnContext(ctx, "massage view identity pending")
			continue
		}
		if err = b.RenderMassage(viewContext, view.owner, view.chat, ""); err != nil {
			b.logger().WarnContext(ctx, "massage view reconciliation pending")
		}
	}
	return nil
}

const massageNoticeCancelled = "cancelled"
