package delivery

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/delivery/dbgen"
)

// controlPacingScope cannot be a canonical numeric Telegram chat or alias.
const controlPacingScope = "$control"
const controlCooldownReason = "delivery_cooldown"

// ControlPacer shares observed bot cooldown with primary delivery. It never owns
// queue entries or chat pacing and never keeps a transaction across HTTP.
type ControlPacer struct {
	db       *pgxpool.Pool
	settings Settings
}

func NewControlPacer(db *pgxpool.Pool, settings Settings) (*ControlPacer, error) {
	if db == nil {
		return nil, ErrSettings
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &ControlPacer{db: db, settings: settings}, nil
}

func (p *ControlPacer) Admit(ctx context.Context) (Admission, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := ReserveControl(ctx, tx, p.settings)
	if err != nil {
		return Admission{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Admission{}, err
	}
	return result, nil
}

func (p *ControlPacer) Observe(ctx context.Context, outcome Outcome) (Outcome, time.Time, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return Outcome{}, time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, deadline, err := ScheduleControl(ctx, tx, p.settings, outcome)
	if err != nil {
		return Outcome{}, time.Time{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Outcome{}, time.Time{}, err
	}
	return result, deadline, nil
}

// ReserveControl locks the shared bot scope, then the reserved control quota.
// A successful prerequisite must not consume its primary delivery reservation.
func ReserveControl(ctx context.Context, tx pgx.Tx, settings Settings) (Admission, error) {
	if err := settings.Validate(); err != nil {
		return Admission{}, err
	}
	q := dbgen.New(tx)
	bot, control, err := lockPacing(ctx, q, settings.BotID, controlPacingScope)
	if err != nil {
		return Admission{}, err
	}
	clock, err := q.DeliveryClock(ctx)
	if err != nil {
		return Admission{}, err
	}
	deadline := clock.Time
	for _, row := range []dbgen.LockPacingRow{bot, control} {
		if row.PauseReason != "" {
			return Admission{Reason: row.PauseReason, NotBefore: clock.Time.Add(settings.Fallback)}, nil
		}
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
		return Admission{Reason: controlCooldownReason, NotBefore: deadline}, nil
	}
	if err = extend(ctx, q, settings.BotID, controlPacingScope, clock.Time.Add(settings.BotInterval), ""); err != nil {
		return Admission{}, err
	}
	return Admission{Ready: true}, nil
}

// ScheduleControl records known shared provider rejection, without touching a
// chat or primary delivery attempt. Invalid delays park the durable bot scope.
func ScheduleControl(ctx context.Context, tx pgx.Tx, settings Settings, outcome Outcome) (Outcome, time.Time, error) {
	if err := settings.Validate(); err != nil {
		return Outcome{}, time.Time{}, err
	}
	if !outcome.Valid() ||
		(outcome.Kind != Parked && outcome.Kind != Paused && (outcome.Kind != Deferred || outcome.Reason != "telegram_rate_limit")) {
		return Outcome{}, time.Time{}, errors.New("invalid control pacing outcome")
	}
	q := dbgen.New(tx)
	if _, err := lockControlPacing(ctx, q, settings.BotID); err != nil {
		return Outcome{}, time.Time{}, err
	}
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
	pause := ""
	if outcome.Kind == Parked || outcome.Kind == Paused {
		pause = outcome.Reason
	}
	if err = extend(ctx, q, settings.BotID, "", deadline, pause); err != nil {
		return Outcome{}, time.Time{}, err
	}
	return outcome, deadline, nil
}

func lockControlPacing(ctx context.Context, q *dbgen.Queries, botID int64) (dbgen.LockPacingRow, error) {
	if err := q.EnsurePacing(ctx, dbgen.EnsurePacingParams{BotID: botID, Chat: ""}); err != nil {
		return dbgen.LockPacingRow{}, err
	}
	return q.LockPacing(ctx, dbgen.LockPacingParams{BotID: botID, Chat: ""})
}
