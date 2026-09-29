package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func isPassBatchUpdate(in incoming, update telegram.Update) bool {
	if update.Message == nil {
		return false
	}
	words := strings.Fields(in.text)
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "/passes_assign", "/passes_cancel", "/passes_uncouple", "/passes_tier":
		return true
	}
	return false
}

func (b *Bot) handlePassBatch(ctx context.Context, in incoming, update telegram.Update) error {
	preferences, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	command, err := b.passBatchInput(ctx, in, update.ID)
	if errors.Is(err, errPassBatchSyntax) {
		return b.sendPassBatchText(ctx, in, preferences.Language, i18n.RegistrationBatchUsage, nil)
	}
	if err != nil {
		return err
	}
	if command.Action == passBatchTier {
		return b.sendPassTier(ctx, in, preferences.Language, command.Event)
	}
	var result []passbooking.RuntimeBatchItem
	err = b.API.call(ctx, in.owner, http.MethodPost, "/v1/passes/batches", command, &result)
	if err != nil {
		return b.passBatchError(ctx, in, preferences.Language, err)
	}
	for _, item := range result {
		status := i18n.RegistrationBatchSucceeded
		if item.Outcome.Status != passbooking.AdminBatchSucceeded {
			status = i18n.RegistrationBatchRejected
		}
		if err = b.sendPassBatchText(
			ctx,
			in,
			preferences.Language,
			status,
			map[string]string{"id": strconv.FormatInt(item.TelegramID, 10), orderCodeParameter: item.Outcome.Code},
		); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) passBatchInput(ctx context.Context, in incoming, id int64) (passbooking.RuntimeBatch, error) {
	var command passbooking.RuntimeBatch
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='pass_batch_input'`, in.owner, id).
		Scan(&command)
	if !errors.Is(err, pgx.ErrNoRows) {
		return command, err
	}
	command, err = parsePassBatch(in.text)
	if err != nil {
		return command, err
	}
	command.Key = "telegram-pass-batch-" + strconv.FormatInt(id, 10)
	if command.Event == "" {
		events, readErr := b.API.PassEvents(ctx, in.owner)
		if readErr != nil {
			return command, readErr
		}
		if len(events) == 0 {
			return command, errPassBatchSyntax
		}
		command.Event = passbooking.ClosestEvent(events, time.Now())
	}
	if err = b.record(ctx, in.owner, id, "pass_batch_input", command); err != nil {
		return command, err
	}
	err = b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='pass_batch_input'`, in.owner, id).
		Scan(&command)
	return command, err
}

func (b *Bot) passBatchError(ctx context.Context, in incoming, language string, err error) error {
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return b.sendPassBatchText(
			ctx,
			in,
			language,
			i18n.RegistrationBatchError,
			map[string]string{orderCodeParameter: problem.Code},
		)
	}
	return err
}

func (b *Bot) sendPassBatchText(
	ctx context.Context,
	in incoming,
	language string,
	id i18n.ID,
	values map[string]string,
) error {
	text, err := i18n.Translate(language, id, values)
	if err != nil {
		return err
	}
	_, err = b.TG.Send(ctx, telegram.Send{ChatID: in.chat, Text: text})
	return err
}

func (b *Bot) sendPassTier(ctx context.Context, in incoming, language, event string) error {
	var result passbooking.TierStatus
	err := b.API.call(ctx, in.owner, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/tiers", nil, &result)
	if err != nil {
		return b.passBatchError(ctx, in, language, err)
	}
	messages, err := passTierMessages(language, result)
	if err != nil {
		return err
	}
	for _, text := range messages {
		if _, err = b.TG.Send(ctx, telegram.Send{ChatID: in.chat, Text: text}); err != nil {
			return err
		}
	}
	return nil
}
