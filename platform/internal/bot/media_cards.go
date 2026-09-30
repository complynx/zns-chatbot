package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) RenderMedia(ctx context.Context, owner string, chat int64, id string) error {
	_, err := b.renderMediaHint(ctx, owner, chat, id)
	return err
}

// Refresh the visible card and give the model that exact question/choice snapshot.
func (b *Bot) renderMediaHint(ctx context.Context, owner string, chat int64, id string) (agent.MediaHint, error) {
	hint := agent.MediaHint{ID: id}
	item, err := b.loadMediaIntake(ctx, owner, id)
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = b.loadMediaOutcome(ctx, owner, id)
	}
	if err != nil {
		return hint, err
	}
	hint.Kind = item.Status
	if err = b.avHint(ctx, owner, &hint); err != nil {
		return hint, err
	}
	pref, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return hint, err
	}
	text, err := i18n.Translate(pref.Language, i18n.MediaTitle, map[string]string{"id": id})
	if err != nil {
		return hint, err
	}
	notice := i18n.ID(item.Notice)
	if notice == "" {
		notice = i18n.MediaPurpose
	}
	message, err := i18n.Translate(pref.Language, notice, nil)
	if err != nil {
		return hint, err
	}
	hint.Question, err = b.mediaModelQuestion(ctx, owner, id, message, item)
	if err != nil {
		return hint, err
	}
	text += "\n" + hint.Question
	hint.Choices, err = b.mediaChoices(ctx, owner, pref.Language, item.Status)
	if err != nil {
		return hint, err
	}
	markup := telegram.Markup{Rows: [][]telegram.Button{}}
	if item.Status != mediaDone {
		markup, err = b.mediaMarkup(ctx, owner, id, hint.Choices)
		if err != nil {
			return hint, err
		}
	}
	if item.Status == mediaChoose {
		if err = b.foodMediaMarkup(ctx, owner, pref.Language, id, &markup, &hint); err != nil {
			return hint, err
		}
	}
	err = b.deliverOrderCard(ctx, owner, mediaPrefix+id, telegram.Send{ChatID: chat, Text: text, Markup: markup})
	if err != nil {
		return hint, err
	}
	// Encode first: only the UPDATE itself is a known SQL operation.
	rendered, err := json.Marshal(hint)
	if err != nil {
		return hint, err
	}
	_, err = b.DB.Exec(ctx, `UPDATE bot.media_intake SET rendered=$3 WHERE owner=$1 AND id=$2`, owner, id, rendered)
	return hint, core.DatabaseOperationError(err)
}

func (b *Bot) mediaMarkup(ctx context.Context, owner, id string, choices []agent.MediaChoice) (telegram.Markup, error) {
	markup := telegram.Markup{Rows: [][]telegram.Button{}}
	for _, choice := range choices {
		button, buttonErr := b.mediaButton(
			ctx,
			owner,
			id,
			choice.Action,
			agent.MediaCandidate{
				OrderID:           choice.OrderID,
				RegistrationEvent: choice.RegistrationEvent,
				Version:           choice.Version,
			},
			choice.Label,
		)
		if buttonErr != nil {
			return markup, buttonErr
		}
		markup.Rows = append(markup.Rows, []telegram.Button{button})
	}
	return markup, nil
}
func (b *Bot) mediaButton(
	ctx context.Context,
	owner, id, action string,
	candidate agent.MediaCandidate,
	label string,
) (telegram.Button, error) {
	token := rand.Text()
	err := b.DB.QueryRow(ctx, `INSERT INTO bot.media_buttons(token,owner,intake_id,action,order_id,version,registration_event)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(owner,intake_id,action,order_id,registration_event,version)
DO UPDATE SET token=bot.media_buttons.token RETURNING token`, token, owner, id, action, candidate.OrderID, candidate.Version, candidate.RegistrationEvent).
		Scan(&token)
	return telegram.Button{Text: label, Data: mediaPrefix + token}, core.DatabaseOperationError(err)
}

func (b *Bot) handleMediaCallback(ctx context.Context, in incoming, update telegram.Update) (resultErr error) {
	// Acknowledgement SQL dominates the handler result; otherwise keep it.
	defer func() {
		if ackErr := b.acknowledge(ctx, update.Callback.ID); ackErr != nil {
			resultErr = ackErr
		}
	}()
	var id, action string
	var candidate agent.MediaCandidate
	err := b.DB.QueryRow(ctx, `SELECT intake_id,action,order_id,version,registration_event FROM bot.media_buttons WHERE owner=$1 AND token=$2`,
		in.owner, strings.TrimPrefix(in.text, mediaPrefix)).
		Scan(&id, &action, &candidate.OrderID, &candidate.Version, &candidate.RegistrationEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return b.mediaNotice(ctx, in, i18n.MediaStale)
	}
	if err != nil {
		return err
	}
	item, expired, err := b.loadMediaUploadState(ctx, in.owner, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return b.mediaNotice(ctx, in, i18n.MediaUnavailable)
	}
	if err != nil {
		return err
	}
	if handled, resumeErr := b.resumeMediaIntake(ctx, in, item, expired); handled {
		return resumeErr
	}
	if action == mediaOrderChoice {
		return b.chooseMediaReceipt(ctx, in, item, candidate, originManual)
	}
	if action == registrationMediaChoice {
		return b.chooseRegistrationReceipt(ctx, in, item, candidate, originManual)
	}
	status, notice := mediaDone, i18n.MediaClosed
	if action == mediaReceipt {
		status, notice = mediaChoose, i18n.MediaChoose
	}
	if action == mediaAvatar {
		notice = i18n.MediaAvatarUnavailable
	}
	_, err = b.DB.Exec(
		ctx,
		`UPDATE bot.media_intake SET status=$3,notice=$4,model_text='',last_action=$5,last_origin='manual' WHERE owner=$1 AND id=$2 AND command IS NULL AND registration_command IS NULL AND food_command IS NULL AND status<>'done'`,
		in.owner,
		id,
		status,
		string(notice),
		action,
	)
	if err != nil {
		return err
	}
	return b.RenderMedia(ctx, in.owner, in.chat, id)
}
