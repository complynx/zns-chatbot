package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
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
	ctx = withAdminMessageSource(ctx, in, update)
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
	result, err := (interaction.RegistrationExecutor{Manual: b.API}).Batch(ctx, in.owner, command, nil)
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
	command, err := (interaction.RegistrationBatchAdmission{
		Store: registrationBatchInputStore{DB: b.DB}, Events: b.API,
	}).Manual(ctx, in.owner, id, func() (passbooking.RuntimeBatch, error) {
		return parsePassBatch(in.text)
	})
	if errors.Is(err, interaction.ErrRegistrationBatchEvent) {
		return passbooking.RuntimeBatch{}, errPassBatchSyntax
	}
	return command, err
}

func (b *Bot) passBatchError(ctx context.Context, in incoming, language string, err error) error {
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
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
	err = b.queueBotUpdateResult(
		ctx,
		in.chat,
		"pass_batch:"+string(id)+":"+values["id"],
		botdelivery.Reference{Family: botFamilyStatic},
		botdelivery.StoredResult{Notice: id, Values: values, Payload: telegram.Send{ChatID: in.chat, Text: text}},
	)
	return err
}

func (b *Bot) sendPassTier(ctx context.Context, in incoming, language, event string) error {
	result, err := b.API.PassTierStatus(ctx, in.owner, event)
	if err != nil {
		return b.passBatchError(ctx, in, language, err)
	}
	messages, err := passTierMessages(language, result)
	if err != nil {
		return err
	}
	for index, text := range messages {
		if err = b.queueBotUpdateResult(
			ctx,
			in.chat,
			"pass_tier:"+event+":"+strconv.Itoa(index),
			botdelivery.Reference{Family: "pass_tier", Event: event},
			botdelivery.StoredResult{Payload: telegram.Send{ChatID: in.chat, Text: text}},
		); err != nil {
			return err
		}
	}
	return nil
}
