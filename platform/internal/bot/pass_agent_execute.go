package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const registrationReply = "registration_reply"

func (b *Bot) registrationExecutionWriter(id int64) interaction.RegistrationExecutionWriter {
	return func(ctx context.Context, owner string, record interaction.RegistrationExecutionRecord) error {
		return b.record(ctx, owner, id, "registration_action", record)
	}
}

func (b *Bot) registrationExecutionNotice(
	ctx context.Context,
	in incoming,
	executionErr error,
) (string, error) {
	if errors.Is(executionErr, interaction.ErrRegistrationExecutionRecord) ||
		(executionErr != nil && passMenuFailure(executionErr) != nil) {
		return "", executionErr
	}
	notice := i18n.RegistrationSaved
	if executionErr != nil {
		notice = i18n.RegistrationStale
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
	cached interaction.SavedPlan,
	notice interaction.Reply,
) error {
	menu, menuErr := b.registrationReplyMenu(ctx, in.owner, id, cached, notice)
	if menuErr != nil {
		return menuErr
	}
	if err := b.storePassMenuWithSource(ctx, in.owner, in.chat, id, menu.RegistrationMenu, menu.Source); err != nil {
		return err
	}
	if err := b.recordReply(
		ctx,
		in.owner,
		id,
		registrationReply,
		notice.Text,
		notice.Origin == interaction.DerivedReply,
		notice.Origin,
	); err != nil {
		return err
	}
	if err := b.finishConsumedVoice(ctx, in.owner, cached); err != nil {
		return err
	}
	return b.RenderPassMenu(ctx, in.owner, in.chat, "")
}

func (b *Bot) registrationReplyMenu(
	ctx context.Context, owner string, id int64, cached interaction.SavedPlan, notice interaction.Reply,
) (botdelivery.PassMenu, error) {
	committedMenu, committed, err := b.committedRegistrationMenu(ctx, owner, id, cached, notice)
	if err != nil {
		return botdelivery.PassMenu{}, err
	}
	if committed {
		var menu botdelivery.PassMenu
		menu.RegistrationMenu = committedMenu
		return menu, nil
	}
	var menu botdelivery.PassMenu
	if cached.RegistrationMenu != nil {
		menu.RegistrationMenu = *cached.RegistrationMenu
	} else {
		menu, _, err = b.passMenuRecord(ctx, owner)
		if err != nil {
			return botdelivery.PassMenu{}, err
		}
	}
	if notice.Origin == interaction.DerivedReply || cached.RegistrationMenu != nil {
		source, sourceErr := registrationMenuSource(owner, cached, menu.Source)
		if sourceErr != nil {
			return botdelivery.PassMenu{}, sourceErr
		}
		menu.Source = &source
	}
	return menu, nil
}

func registrationMenuSource(
	owner string,
	cached interaction.SavedPlan,
	copied *readsource.Derivation,
) (readsource.Derivation, error) {
	value, err := savedPlanSource(cached)
	if err != nil {
		return value, err
	}
	if copied == nil {
		return value, nil
	}
	original, err := readsource.Capture(owner, *copied)
	if err != nil {
		return value, err
	}
	value.Authorities, err = readsource.Merge(value.Authorities, original)
	if err != nil {
		return value, err
	}
	value.PrivateHistory = value.PrivateHistory || copied.PrivateHistory
	return value, nil
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
		return payload, core.DatabaseOperationError(err)
	}
	visible, err := b.derivedReplyVisible(ctx, owner, revision)
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
