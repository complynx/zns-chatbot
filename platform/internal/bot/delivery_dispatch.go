package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const deliveryPassLimit = 100

// dispatchDeliveryPass reselects after each owner attempt. A failed or stale
// advisory head is attempted once per pass, so other available lanes can run.
func dispatchDeliveryPass(
	ctx context.Context,
	selectHeads func(context.Context) ([]delivery.Entry, error),
	dispatch func(context.Context, delivery.Reference) error,
) error {
	attempted := make(map[delivery.Reference]bool)
	var failures []error
	for len(attempted) < deliveryPassLimit {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		heads, err := selectHeads(ctx)
		if err != nil {
			return errors.Join(append(failures, deliveryFailure(err))...)
		}
		if err = ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		next, found := nextDeliveryHead(heads, attempted)
		if !found {
			break
		}
		attempted[next] = true
		if err = dispatch(ctx, next); err != nil {
			failures = append(failures, deliveryFailure(err))
			if core.IsDatabaseFailure(err) {
				return errors.Join(append(failures, ctx.Err())...)
			}
		}
	}
	return errors.Join(failures...)
}

func nextDeliveryHead(heads []delivery.Entry, attempted map[delivery.Reference]bool) (delivery.Reference, bool) {
	for _, head := range heads {
		if !attempted[head.Reference] {
			return head.Reference, true
		}
	}
	return delivery.Reference{}, false
}

func deliveryFailure(err error) error {
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	return err
}

func (b *Bot) deliveryHeads(ctx context.Context) ([]delivery.Entry, error) {
	tx, err := b.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	heads, err := delivery.Candidates(ctx, tx, b.Delivery.BotID, deliveryPassLimit)
	if err != nil {
		return nil, err
	}
	// No selection transaction remains open while an owner acquires its locks
	// or calls Telegram. The owner revalidates this advisory reference.
	if err = tx.Commit(ctx); err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	return heads, nil
}

func (b *Bot) dispatchDelivery(ctx context.Context, ref delivery.Reference) error {
	if ref.Owner == delivery.Bot {
		return b.DeliverBotIntent(ctx, ref)
	}
	id, err := strconv.ParseInt(ref.Key, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != ref.Key || ref.Effect != botPhaseSend {
		return delivery.ErrQueueReference
	}
	switch ref.Owner {
	case delivery.Bot:
		return delivery.ErrQueueReference
	case delivery.Orders:
		return b.DeliverOrderNotification(ctx, id)
	case delivery.Passes:
		return b.DeliverPassNotification(ctx, id)
	case delivery.Food:
		return b.DeliverFoodNotification(ctx, id)
	case delivery.Massage:
		return b.DeliverMassageNotification(ctx, id)
	case delivery.Admin:
		return b.deliverAdminMessageID(ctx, id)
	case delivery.Announcement:
		return b.deliverAnnouncementID(ctx, id)
	default:
		return delivery.ErrQueueReference
	}
}

func (b *Bot) deliverAdminMessageID(ctx context.Context, id int64) error {
	item, found, err := b.Host.PrepareAdminMessage(ctx, id)
	if err != nil || !found {
		return err
	}
	return b.deliverPreparedAdminMessage(ctx, item)
}

func (b *Bot) deliverAnnouncementID(ctx context.Context, id int64) error {
	item, found, err := b.Host.PrepareRegistrationAnnouncement(ctx, id)
	if err != nil || !found {
		return err
	}
	return b.deliverPreparedRegistrationAnnouncement(ctx, item)
}

// Run calls this only from its joined serial worker under exclusive bot ownership.
func (b *Bot) dispatchQueuedDeliveries(ctx context.Context) error {
	steps := []func(context.Context) error{
		b.RecoverBotIntents,
		b.RecoverOrderNotifications,
		b.RecoverPassNotifications,
		b.RecoverFoodNotifications,
		b.RecoverMassageNotifications,
		b.Host.RecoverAdminMessages,
		b.Host.RecoverRegistrationAnnouncements,
		b.deliverAdminInputExpiry,
		b.ContinueBotIntentReceipts,
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := step(ctx); err != nil {
			if core.IsDatabaseFailure(err) {
				return core.ErrDatabase
			}
			b.logger().WarnContext(ctx, "delivery recovery pending", "error", err)
		}
	}
	if err := dispatchDeliveryPass(ctx, b.deliveryHeads, b.dispatchDelivery); err != nil {
		if core.IsDatabaseFailure(err) {
			return core.ErrDatabase
		}
		if ctx.Err() == nil {
			b.logger().WarnContext(ctx, "delivery pass incomplete", "error", err)
		}
	}
	return nil
}
