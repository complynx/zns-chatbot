package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrTelegramUnavailable = errors.New("telegram recipient unavailable")

type TelegramAvailability struct {
	Revision int64
	Reason   string
}

// TelegramAvailability is transport state, separate from identity, bans and business state.
// An absent row is reachable. Bot scoping also covers users not yet linked to IAM.
func (s *Store) TelegramAvailability(ctx context.Context, botID, telegramID int64) (TelegramAvailability, error) {
	var state TelegramAvailability
	err := s.pool.QueryRow(ctx, `SELECT revision,reason FROM shuttle_telegram_availability
 WHERE bot_id=$1 AND telegram_user_id=$2`, botID, telegramID).Scan(&state.Revision, &state.Reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	return state, err
}

// SuspendTelegram ignores an old send failure after a newer inbound update committed.
func (s *Store) SuspendTelegram(ctx context.Context, botID, telegramID, revision int64, reason string) error {
	if botID <= 0 || telegramID <= 0 || revision < 0 || (reason != "user_deactivated" && reason != "bot_blocked") {
		return errors.New("invalid Telegram suspension")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO shuttle_telegram_availability
 (bot_id,telegram_user_id,revision,reason) VALUES($1,$2,$3,$4)
 ON CONFLICT (bot_id,telegram_user_id) DO UPDATE SET reason=excluded.reason,updated_at=statement_timestamp()
 WHERE shuttle_telegram_availability.revision=excluded.revision`, botID, telegramID, revision, reason)
	return err
}

// Only the fresh, transactionally deduplicated private Telegram intake calls this.
// A committed reply increments the fence even if the recipient was already reachable.
func resumeTelegram(ctx context.Context, tx pgx.Tx, r IntakeRequest) error {
	if r.ChatID != r.TelegramUserID || r.TelegramUserID <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO shuttle_telegram_availability
 (bot_id,telegram_user_id,revision) VALUES($1,$2,1)
 ON CONFLICT (bot_id,telegram_user_id) DO UPDATE SET reason='',
 revision=shuttle_telegram_availability.revision+1,updated_at=statement_timestamp()`, r.BotID, r.TelegramUserID)
	return err
}

func (s *Store) CheckTelegramAvailability(ctx context.Context, botID, telegramID int64) error {
	state, err := s.TelegramAvailability(ctx, botID, telegramID)
	if err == nil && state.Reason != "" {
		return ErrTelegramUnavailable
	}
	return err
}
