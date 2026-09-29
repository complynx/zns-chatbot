package delivery

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/delivery/dbgen"
)

// Reserve holds only short pacing locks inside the caller's send-boundary transaction.
func Reserve(ctx context.Context, tx pgx.Tx, settings Settings, destination Destination) (Admission, error) {
	if err := settings.Validate(); err != nil {
		return Admission{}, err
	}
	if destination.Chat == "" {
		return Admission{}, errors.New("delivery destination missing")
	}
	q := dbgen.New(tx)
	bot, chat, err := lockPacing(ctx, q, settings.BotID, destination.Chat)
	if err != nil {
		return Admission{}, err
	}
	clock, err := q.DeliveryClock(ctx)
	if err != nil {
		return Admission{}, err
	}
	if bot.PauseReason != "" {
		return Admission{Reason: bot.PauseReason, NotBefore: clock.Time.Add(settings.Fallback)}, nil
	}
	if chat.PauseReason != "" {
		return Admission{Reason: chat.PauseReason, NotBefore: clock.Time.Add(settings.Fallback)}, nil
	}
	deadline := clock.Time
	for _, row := range []dbgen.LockPacingRow{bot, chat} {
		if row.NotBefore.InfinityModifier == pgtype.Infinity {
			return Admission{
				Reason:    "delivery_deadline_unrepresentable",
				NotBefore: clock.Time.Add(settings.Fallback),
			}, nil
		}
		if row.NotBefore.Valid && row.NotBefore.Time.After(deadline) {
			deadline = row.NotBefore.Time
		}
	}
	if deadline.After(clock.Time) {
		return Admission{NotBefore: deadline, Reason: "delivery_cooldown"}, nil
	}
	if err = extend(ctx, q, settings.BotID, "", clock.Time.Add(settings.BotInterval), ""); err != nil {
		return Admission{}, err
	}
	if err = extend(ctx, q, settings.BotID, destination.Chat, clock.Time.Add(settings.ChatInterval), ""); err != nil {
		return Admission{}, err
	}
	return Admission{Ready: true}, nil
}

// Schedule records an observed transport outcome in the same transaction as its outbox.
// A 429 with unspecified scope extends both bot and chat without consuming a failure budget.
func Schedule(
	ctx context.Context,
	tx pgx.Tx,
	settings Settings,
	destination Destination,
	outcome Outcome,
) (Outcome, time.Time, error) {
	q := dbgen.New(tx)
	clock, err := q.DeliveryClock(ctx)
	if err != nil {
		return Outcome{}, time.Time{}, err
	}
	deadline := clock.Time
	if outcome.Kind == Deferred {
		var valid bool
		deadline, valid = settings.Cooldown(clock.Time, outcome)
		if !valid {
			outcome = Outcome{Kind: Parked, Reason: "telegram_invalid_cooldown"}
			deadline = clock.Time
		}
	}
	shared := outcome.Reason == "telegram_rate_limit" || outcome.Kind == Paused || outcome.Kind == Parked
	if !shared {
		return outcome, deadline, nil
	}
	if err = settings.Validate(); err != nil {
		return Outcome{}, time.Time{}, err
	}
	if _, _, err = lockPacing(ctx, q, settings.BotID, destination.Chat); err != nil {
		return Outcome{}, time.Time{}, err
	}
	pause := ""
	if outcome.Kind == Paused || outcome.Kind == Parked {
		pause = outcome.Reason
	}
	if err = extend(ctx, q, settings.BotID, "", deadline, pause); err != nil {
		return Outcome{}, time.Time{}, err
	}
	if err = extend(ctx, q, settings.BotID, destination.Chat, deadline, pause); err != nil {
		return Outcome{}, time.Time{}, err
	}
	return outcome, deadline, nil
}

func lockPacing(
	ctx context.Context,
	q *dbgen.Queries,
	botID int64,
	chat string,
) (dbgen.LockPacingRow, dbgen.LockPacingRow, error) {
	var bot, row dbgen.LockPacingRow
	if err := q.EnsurePacing(ctx, dbgen.EnsurePacingParams{BotID: botID, Chat: ""}); err != nil {
		return bot, row, err
	}
	var err error
	bot, err = q.LockPacing(ctx, dbgen.LockPacingParams{BotID: botID, Chat: ""})
	if err != nil {
		return bot, row, err
	}
	if err = q.EnsurePacing(ctx, dbgen.EnsurePacingParams{BotID: botID, Chat: chat}); err != nil {
		return bot, row, err
	}
	row, err = q.LockPacing(ctx, dbgen.LockPacingParams{BotID: botID, Chat: chat})
	return bot, row, err
}

func extend(ctx context.Context, q *dbgen.Queries, botID int64, chat string, deadline time.Time, reason string) error {
	return q.ExtendPacing(
		ctx,
		dbgen.ExtendPacingParams{
			BotID:       botID,
			Chat:        chat,
			NotBefore:   pgtype.Timestamptz{Time: deadline, Valid: true},
			PauseReason: reason,
		},
	)
}
