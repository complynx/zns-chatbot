package task

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const telegramAvailabilityPoll = time.Minute

// All private-chat effects share one gate, including cosmetic actions and native media.
// File reads, bot-level menu clearing and callback acknowledgements have no recipient effect.
type taskTelegram interface {
	SendMessage(context.Context, telegram.SendMessageRequest) (telegram.Receipt, error)
	SendDocument(context.Context, telegram.SendDocumentRequest) (telegram.Receipt, error)
	ReadFile(context.Context, string, int64) ([]byte, error)
	SetChatCommands(context.Context, int64, string, []telegram.BotCommand) error
	SetChatMenuCommands(context.Context, int64) error
	ClearDefaultCommands(context.Context, string, string) error
	SendActivity(context.Context, int64, bool) error
	SetMessageReaction(context.Context, int64, int64, string) error
	AnswerCallbackQuery(context.Context, string, string) error
}

type availableTelegram struct {
	*telegram.Client

	store  *db.Store
	botID  int64
	logger *slog.Logger
}

func (c *availableTelegram) privateCall(ctx context.Context, id int64, call func() error) error {
	if id <= 0 {
		return call()
	}
	state, err := c.store.TelegramAvailability(ctx, c.botID, id)
	if err != nil || state.Reason != "" {
		return &telegram.Error{Outcome: telegram.NotStarted, Retryable: true, RetryAfter: telegramAvailabilityPoll}
	}
	err = call()
	failure, ok := errors.AsType[*telegram.Error](err)
	if !ok || failure.Outcome != telegram.Rejected || failure.StatusCode != http.StatusForbidden {
		return err
	}
	reason := failure.ReasonClass()
	if reason != "user_deactivated" && reason != "bot_blocked" {
		return err
	}
	// Persist a definite rejection even if the request's context was cancelled afterward.
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if saveErr := c.store.SuspendTelegram(finish, c.botID, id, state.Revision, reason); saveErr != nil && c.logger != nil {
		c.logger.WarnContext(finish, "Telegram availability persistence unavailable", "class", "telegram_availability")
	}
	return err
}

func (c *availableTelegram) SendMessage(ctx context.Context, in telegram.SendMessageRequest) (telegram.Receipt, error) {
	var result telegram.Receipt
	err := c.privateCall(
		ctx,
		in.ChatID,
		func() error { var callErr error; result, callErr = c.Client.SendMessage(ctx, in); return callErr },
	)
	return result, err
}

func (c *availableTelegram) SendDocument(
	ctx context.Context,
	in telegram.SendDocumentRequest,
) (telegram.Receipt, error) {
	var result telegram.Receipt
	err := c.privateCall(
		ctx,
		in.ChatID,
		func() error { var callErr error; result, callErr = c.Client.SendDocument(ctx, in); return callErr },
	)
	return result, err
}
func (c *availableTelegram) SetChatCommands(
	ctx context.Context, id int64, language string, commands []telegram.BotCommand,
) error {
	return c.privateCall(ctx, id, func() error { return c.Client.SetChatCommands(ctx, id, language, commands) })
}
func (c *availableTelegram) SetChatMenuCommands(ctx context.Context, id int64) error {
	return c.privateCall(ctx, id, func() error { return c.Client.SetChatMenuCommands(ctx, id) })
}
func (c *availableTelegram) SendActivity(ctx context.Context, id int64, choosing bool) error {
	return c.privateCall(ctx, id, func() error { return c.Client.SendActivity(ctx, id, choosing) })
}
func (c *availableTelegram) SetMessageReaction(ctx context.Context, id, message int64, emoji string) error {
	return c.privateCall(ctx, id, func() error { return c.Client.SetMessageReaction(ctx, id, message, emoji) })
}

func (s *Service) telegramUserAvailable(ctx context.Context, u db.User) error {
	return s.store.CheckTelegramAvailability(ctx, u.BotID, u.TelegramUserID)
}

func (s *Service) notificationRetry(err error) error {
	if errors.Is(err, db.ErrTelegramUnavailable) {
		return river.JobSnooze(telegramAvailabilityPoll)
	}
	return river.JobSnooze(s.options.PollInterval)
}

// A paused transport does not start another agent attempt. Already-dispatched external
// work may finish; its receipts and business effects are never rolled back here.
func (s *Service) taskTelegramAvailable(ctx context.Context, id uuid.UUID) error {
	task, err := s.store.GetTask(ctx, id)
	if err != nil {
		return s.wait()
	}
	owner, err := s.store.GetUser(ctx, task.UserID)
	if err != nil {
		return s.wait()
	}
	if err = s.telegramUserAvailable(ctx, owner); err != nil {
		return s.notificationRetry(err)
	}
	return nil
}
