package delivery

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery/dbgen"
)

// Reserve holds only short pacing locks inside the caller's send-boundary transaction.
func Reserve(ctx context.Context, tx pgx.Tx, settings Settings, destination Destination) (Admission, error) {
	gate, clock, err := observePacing(ctx, tx, settings, destination)
	if err != nil || !gate.Ready {
		return gate, err
	}
	q := dbgen.New(tx)
	if err = extend(ctx, q, settings.BotID, "", clock.Add(settings.BotInterval), ""); err != nil {
		return Admission{}, err
	}
	if err = extend(ctx, q, settings.BotID, destination.Chat, clock.Add(settings.ChatInterval), ""); err != nil {
		return Admission{}, err
	}
	return gate, nil
}

// observePacing holds the existing bot/chat locks without reserving a send interval.
func observePacing(
	ctx context.Context,
	tx pgx.Tx,
	settings Settings,
	destination Destination,
) (Admission, time.Time, error) {
	if err := settings.Validate(); err != nil {
		return Admission{}, time.Time{}, err
	}
	if destination.Chat == "" {
		return Admission{}, time.Time{}, errors.New("delivery destination missing")
	}
	q := dbgen.New(tx)
	bot, chat, err := lockPacing(ctx, q, settings.BotID, destination.Chat)
	if err != nil {
		return Admission{}, time.Time{}, err
	}
	clock, err := q.DeliveryClock(ctx)
	if err != nil {
		return Admission{}, time.Time{}, core.DatabaseOperationError(err)
	}
	if bot.PauseReason != "" {
		return Admission{Reason: bot.PauseReason, NotBefore: clock.Time.Add(settings.Fallback)}, clock.Time, nil
	}
	if chat.PauseReason != "" {
		return Admission{Reason: chat.PauseReason, NotBefore: clock.Time.Add(settings.Fallback)}, clock.Time, nil
	}
	deadline := clock.Time
	for _, row := range []dbgen.LockPacingRow{bot, chat} {
		if row.NotBefore.InfinityModifier == pgtype.Infinity {
			return Admission{
				Reason:    "delivery_deadline_unrepresentable",
				NotBefore: clock.Time.Add(settings.Fallback),
			}, clock.Time, nil
		}
		if row.NotBefore.Valid && row.NotBefore.Time.After(deadline) {
			deadline = row.NotBefore.Time
		}
	}
	if deadline.After(clock.Time) {
		return Admission{NotBefore: deadline, Reason: "delivery_cooldown"}, clock.Time, nil
	}
	return Admission{Ready: true}, clock.Time, nil
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
		return Outcome{}, time.Time{}, core.DatabaseOperationError(err)
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
		return bot, row, core.DatabaseOperationError(err)
	}
	var err error
	bot, err = q.LockPacing(ctx, dbgen.LockPacingParams{BotID: botID, Chat: ""})
	if err != nil {
		return bot, row, core.DatabaseOperationError(err)
	}
	if err = q.EnsurePacing(ctx, dbgen.EnsurePacingParams{BotID: botID, Chat: chat}); err != nil {
		return bot, row, core.DatabaseOperationError(err)
	}
	row, err = q.LockPacing(ctx, dbgen.LockPacingParams{BotID: botID, Chat: chat})
	return bot, row, core.DatabaseOperationError(err)
}

func extend(ctx context.Context, q *dbgen.Queries, botID int64, chat string, deadline time.Time, reason string) error {
	err := q.ExtendPacing(
		ctx,
		dbgen.ExtendPacingParams{
			BotID:       botID,
			Chat:        chat,
			NotBefore:   pgtype.Timestamptz{Time: deadline, Valid: true},
			PauseReason: reason,
		},
	)
	return core.DatabaseOperationError(err)
}
