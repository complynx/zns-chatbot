package bot

import (
	"context"
	"errors"
	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/bot/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) massageNotificationWire(ctx context.Context, owner string, chat int64, value massage.DeliveryNotice) (*notificationwire.Payload, error) {
	if value.Notice.Wire != nil {
		return value.Notice.Wire, nil
	}
	send, err := b.massageNotificationPayload(ctx, owner, chat, value)
	if err != nil {
		return nil, err
	}
	return notificationWireCandidate(send)
}

// DeliverMassageNotifications isolates recipient failures and retains wire uncertainty.
func (b *Bot) DeliverMassageNotifications(ctx context.Context) error {
	recipients, err := b.Host.MassageNoticeRecipients(ctx)
	if err != nil {
		return err
	}
	var failures error
	delivered := 0
	for _, recipient := range recipients {
		if delivered >= massageDeliveryBatch {
			break
		}
		notices, noticeErr := b.Host.MassageDeliveryNotices(ctx, recipient.Owner)
		if noticeErr != nil {
			failures = errors.Join(failures, deliveryFailure(noticeErr))
			if core.IsDatabaseFailure(noticeErr) || ctx.Err() != nil {
				return errors.Join(failures, ctx.Err())
			}
			continue
		}
		for _, notice := range notices {
			if delivered >= massageDeliveryBatch {
				break
			}
			delivered++
			err = b.deliverMassageNotice(ctx, recipient.Owner, notice.TelegramID, notice)
			failures = errors.Join(failures, deliveryFailure(err))
			if core.IsDatabaseFailure(err) || ctx.Err() != nil {
				return errors.Join(failures, ctx.Err())
			}
		}
	}
	return failures
}

const massageDeliveryBatch = 10

func (b *Bot) deferMassageNotification(ctx context.Context, owner string, n massage.Notice, err error) error {
	if core.IsDatabaseFailure(err) {
		return err
	}
	if n.FollowupPending {
		done, reason := notificationFollowupResult(err)
		return b.Host.CompleteMassageNoticeFollowup(
			ctx,
			owner,
			massage.NotificationFollowup{ID: n.ID, Attempt: n.DeliveryAttempt, Done: done, Failure: reason},
		)
	}
	return b.Host.CompleteMassageNotice(
		ctx,
		owner,
		massage.NotificationCompletion{
			ID:      n.ID,
			Attempt: n.DeliveryAttempt,
			Outcome: notificationPreflightOutcome(err),
		},
	)
}

func (b *Bot) deliverMassageNotice(ctx context.Context, owner string, chat int64, value massage.DeliveryNotice) error {
	notice := value.Notice
	if !value.Current {
		if notice.FollowupPending {
			return b.Host.CompleteMassageNoticeFollowup(
				ctx,
				owner,
				massage.NotificationFollowup{
					ID:      notice.ID,
					Attempt: notice.DeliveryAttempt,
					Done:    true,
					Failure: notificationNoLongerCurrent,
				},
			)
		}
		return b.Host.CompleteMassageNotice(
			ctx,
			owner,
			massage.NotificationCompletion{
				ID:      notice.ID,
				Attempt: notice.DeliveryAttempt,
				Outcome: delivery.Outcome{Kind: delivery.Cancelled, Reason: notificationNoLongerCurrent},
			},
		)
	}
	authenticated, err := b.API.NotificationContext(ctx, owner, chat)
	if err != nil {
		return b.deferMassageNotification(ctx, owner, notice, err)
	}
	ctx = authenticated
	var known bool
	notice, known, err = b.sendPreparedMassageNotice(ctx, owner, chat, value)
	if err != nil || !known {
		return err
	}
	ctx = withBotDeliveryOrigin(
		ctx,
		delivery.Reference{Owner: delivery.Massage, Key: strconv.FormatInt(notice.ID, 10), Effect: botRefreshEffect},
	)
	err = b.followupMassageNotification(ctx, owner, chat, notice)
	if core.IsDatabaseFailure(err) {
		return err
	}
	done, reason := notificationFollowupResult(err)
	completeErr := b.Host.CompleteMassageNoticeFollowup(
		ctx,
		owner,
		massage.NotificationFollowup{ID: notice.ID, Attempt: notice.DeliveryAttempt, Done: done, Failure: reason},
	)
	return errors.Join(err, completeErr)
}

func (b *Bot) massageNotificationPayload(
	ctx context.Context,
	owner string,
	chat int64,
	value massage.DeliveryNotice,
) (telegram.Send, error) {
	pref, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return telegram.Send{}, err
	}
	title := i18n.MassageReminder
	switch value.Notice.Kind {
	case "booked":
		title = i18n.MassageNewBooking
	case massageNoticeCancelled:
		title = i18n.MassageCancelNotice
	}
	r := massageRenderer{language: pref.Language}
	text := r.text(
		title,
	) + "\n" + massageWhen(
		value.Reservation.Start,
	) + " · " + r.reservationQuote(
		value.Reservation,
	) + "\n" + massageName(
		value.Specialist,
	) + " · " + massageName(
		value.Client,
	)
	return telegram.Send{
		ChatID: chat,
		Text:   text,
		Markup: telegram.Markup{
			Rows: [][]telegram.Button{{{Text: r.text(i18n.MassageHome), Data: massagePrefix + "open"}}},
		},
	}, nil
}

func (b *Bot) followupMassageNotification(ctx context.Context, owner string, chat int64, notice massage.Notice) error {
	q := dbgen.New(b.DB)
	if err := q.StoreMassageNotificationReceipt(
		ctx,
		dbgen.StoreMassageNotificationReceiptParams{ID: notice.ID, MessageID: notice.MessageID},
	); err != nil {
		return core.DatabaseOperationError(err)
	}
	opened, err := q.MassageNotificationViewOpened(ctx, owner)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if opened {
		return b.RenderMassage(ctx, owner, chat, "")
	}
	return nil
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
		return core.DatabaseOperationError(err)
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
		return core.DatabaseOperationError(err)
	}
	for _, view := range views {
		viewContext, authErr := b.API.NotificationContext(ctx, view.owner, view.chat)
		if authErr != nil {
			if failure := reconcileDatabaseFailure(authErr); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "massage view identity pending")
			continue
		}
		if err = b.RenderMassage(viewContext, view.owner, view.chat, ""); err != nil {
			if failure := reconcileDatabaseFailure(err); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "massage view reconciliation pending")
		}
	}
	return nil
}

const massageNoticeCancelled = "cancelled"

// A known canonical outcome permits follow-up; deferred or rejected work does not.
func (b *Bot) sendPreparedMassageNotice(
	ctx context.Context,
	owner string,
	chat int64,
	value massage.DeliveryNotice,
) (massage.Notice, bool, error) {
	notice := value.Notice
	if notice.FollowupPending {
		return notice, true, nil
	}
	wire, err := b.massageNotificationWire(ctx, owner, chat, value)
	if err != nil {
		return notice, false, b.deferMassageNotification(ctx, owner, notice, err)
	}
	gate, err := b.Host.BeginMassageNotice(
		ctx,
		owner,
		massage.NotificationAttempt{Attempt: delivery.Attempt{ID: notice.ID, Generation: notice.DeliveryAttempt}, Wire: wire},
	)
	if err != nil || !gate.Ready {
		return notice, false, err
	}
	payload, err := notificationWireSend(chat, gate.Wire)
	if err != nil {
		return notice, false, err
	}
	message, sendErr := b.TG.Send(ctx, payload)
	outcome := telegram.DeliveryOutcome(message.ID, sendErr)
	result := massage.NotificationCompletion{ID: notice.ID, Attempt: notice.DeliveryAttempt, Outcome: outcome}
	if outcome.Kind == delivery.Succeeded {
		result.Text = payload.Text
	}
	completionCtx, cancelCompletion := deliveryCompletionContext(ctx)
	defer cancelCompletion()
	if err = b.Host.CompleteMassageNotice(completionCtx, owner, result); err != nil {
		return notice, false, err
	}
	if outcome.Kind != delivery.Succeeded {
		return notice, false, nil
	}
	notice.MessageID = message.ID
	return notice, true, nil
}

// DeliverMassageNotification handles one advisory shared-queue candidate.
func (b *Bot) DeliverMassageNotification(ctx context.Context, id int64) error {
	notice, found, err := b.Host.PrepareMassageNotification(ctx, id)
	if err != nil || !found {
		return err
	}
	return b.deliverMassageNotice(ctx, notice.Owner, notice.TelegramID, notice)
}

// RecoverMassageNotifications never starts a fresh primary send.
func (b *Bot) RecoverMassageNotifications(ctx context.Context) error {
	notices, err := b.Host.RecoverMassageNotifications(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, notice := range notices {
		err = b.deliverMassageNotice(ctx, notice.Owner, notice.TelegramID, notice)
		failures = errors.Join(failures, deliveryFailure(err))
		if core.IsDatabaseFailure(err) || ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}
