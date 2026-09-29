package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const registrationReply = "registration_reply"

func (b *Bot) executeRegistrationCommand(
	ctx context.Context,
	in incoming,
	id int64,
	command passbooking.Command,
) (string, error) {
	command.Key = "tg-registration-" + strconv.FormatInt(id, 10)
	_, executionErr := b.API.ExecutePassBooking(ctx, in.owner, command)
	return b.registrationExecutionNotice(ctx, in, id, command.Name, command.Event, executionErr)
}

func (b *Bot) registrationExecutionNotice(
	ctx context.Context,
	in incoming,
	id int64,
	name, event string,
	executionErr error,
) (string, error) {
	if executionErr != nil && passMenuFailure(executionErr) != nil {
		return "", executionErr
	}
	notice := i18n.RegistrationSaved
	if executionErr != nil {
		notice = i18n.RegistrationStale
	}
	if err := b.record(
		ctx,
		in.owner,
		id,
		"registration_action",
		map[string]string{actionField: name, knowledgeEventQuery: event},
	); err != nil {
		return "", err
	}
	prefs, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", err
	}
	return i18n.Translate(prefs.Language, notice, nil)
}

// The reply is bound to the menu revision. Later manual navigation retires it
// while the conversation archive still preserves the assistant's answer.
func (b *Bot) finishRegistrationReply(
	ctx context.Context,
	in incoming,
	id int64,
	cached cachedPlan,
	notice string,
) error {
	state := cached.RegistrationMenu
	if state == nil {
		current, _, err := b.passMenuState(ctx, in.owner)
		if err != nil {
			return err
		}
		state = &current
	}
	if err := b.storePassMenu(ctx, in.owner, in.chat, id, *state); err != nil {
		return err
	}
	if err := b.recordReply(
		ctx,
		in.owner,
		id,
		registrationReply,
		notice,
		cached.RegistrationCommand == nil && cached.RegistrationAssignment == nil,
	); err != nil {
		return err
	}
	if err := b.finishConsumedVoice(ctx, in.owner, cached); err != nil {
		return err
	}
	return b.RenderPassMenu(ctx, in.owner, in.chat, "")
}

func (b *Bot) registrationReplyPayload(
	ctx context.Context,
	owner string,
	revision int64,
	payload telegram.Send,
) (telegram.Send, error) {
	var text string
	var native bool
	err := b.DB.QueryRow(ctx, `SELECT content#>>'{}',native_markdown FROM bot.interactions
	WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, revision, registrationReply).Scan(&text, &native)
	if errors.Is(err, pgx.ErrNoRows) {
		return payload, nil
	}
	if err != nil {
		return payload, err
	}
	visible, err := b.historyReplyVisible(ctx, owner, revision)
	if err != nil || !visible {
		return payload, err
	}
	if text != "" {
		if native {
			payload.Text, payload.LiteralSuffix, payload.NativeMarkdown = text, "\n\n"+payload.Text, true
		} else {
			payload.Text = text + "\n\n" + payload.Text
		}
	}
	return payload, nil
}
