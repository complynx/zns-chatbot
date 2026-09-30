package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const registrationMediaChoice = "registration"

func resolvedRegistrationReceipt(in incoming, plan agent.Plan, input agent.Input) (string, int64) {
	p := plan.MediaAction
	if p == nil || p.Intent != mediaReceipt || p.RegistrationEvent == "" || input.MediaContext == nil {
		return "", 0
	}
	evidence, _ := receiptFollowupEvidence(in, input, p.MediaID)
	if evidence == "" {
		return "", 0
	}
	for _, hint := range input.MediaContext.Pending {
		if hint.ID != p.MediaID {
			continue
		}
		for _, choice := range hint.Choices {
			if choice.Action == registrationMediaChoice && choice.RegistrationEvent == p.RegistrationEvent {
				return choice.RegistrationEvent, choice.Version
			}
		}
	}
	return "", 0
}

func (b *Bot) registrationMediaCandidates(ctx context.Context, owner string) ([]agent.MediaCandidate, error) {
	events, err := b.API.PassEvents(ctx, owner)
	if err != nil {
		return nil, err
	}
	var candidates []agent.MediaCandidate
	for _, event := range events {
		quote, quoteErr := b.API.PassPaymentQuote(ctx, owner, event.ID)
		if quoteErr != nil {
			if passMenuFailure(quoteErr) != nil {
				return nil, quoteErr
			}
			continue
		}
		candidates = append(candidates, agent.MediaCandidate{
			RegistrationEvent: event.ID,
			RegistrationTitles: map[string]string{
				"en": passMenuLabel(event.Titles["en"]),
				"ru": passMenuLabel(event.Titles["ru"]),
			},
			Version:   quote.Version,
			AmountRUB: strconv.FormatInt(quote.Total, 10),
		})
	}
	return candidates, nil
}

func (b *Bot) chooseRegistrationReceipt(
	ctx context.Context,
	in incoming,
	item mediaIntake,
	candidate agent.MediaCandidate,
	origin string,
) error {
	return b.chooseRegistrationReceiptWithSource(ctx, in, item, candidate, origin, nil)
}

func (b *Bot) chooseRegistrationReceiptWithSource(ctx context.Context, in incoming, item mediaIntake,
	candidate agent.MediaCandidate, origin string, source *readsource.Derivation) error {
	boundSource, sourceErr := mediaCommandSource(origin, source)
	if sourceErr != nil {
		return sourceErr
	}
	if source != nil {
		source = &boundSource
	}
	command := passbooking.Command{Name: stateProof, Event: candidate.RegistrationEvent, Version: candidate.Version,
		Key: "media-" + item.ID}
	err := b.DB.QueryRow(ctx, `UPDATE bot.media_intake SET registration_command=$3,last_action='select_registration',last_origin=$4,command_source=$5
WHERE owner=$1 AND id=$2 AND command IS NULL AND registration_command IS NULL AND food_command IS NULL AND status<>'done' AND expires_at>now()
RETURNING registration_command,command_source,last_origin`, in.owner, item.ID, command, origin, source).
		Scan(&item.RegistrationCommand, &item.CommandSource, &item.CommandOrigin)
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = b.loadMediaIntake(ctx, in.owner, item.ID)
	}
	if err != nil {
		return err
	}
	if item.RegistrationCommand == nil {
		return b.RenderMedia(ctx, in.owner, in.chat, item.ID)
	}
	return b.commitRegistrationReceipt(ctx, in, item)
}

func (b *Bot) commitRegistrationReceipt(ctx context.Context, in incoming, item mediaIntake) error {
	if _, err := mediaCommandSource(item.CommandOrigin, item.CommandSource); err != nil {
		return b.mediaExecutionError(ctx, in, item, err)
	}
	command := *item.RegistrationCommand
	if command.ProofID == "" {
		// Promotion uses the shared immutable owner-bound proof store; no file is
		// attached to a booking until its versioned domain command succeeds.
		proof, err := b.API.PromoteMedia(ctx, in.owner, item.AttachmentID)
		if err != nil {
			return b.mediaExecutionError(ctx, in, item, err)
		}
		command.ProofID = proof.ID
		encoded, encodeErr := json.Marshal(command)
		if encodeErr != nil {
			return encodeErr
		}
		if _, err = b.DB.Exec(
			ctx,
			`UPDATE bot.media_intake SET registration_command=$3 WHERE owner=$1 AND id=$2`,
			in.owner,
			item.ID,
			encoded,
		); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	item.RegistrationCommand = &command
	_, executionErr := (interaction.RegistrationExecutor{Manual: b.API, Derived: b.Host}).
		Command(ctx, in.owner, command, item.CommandSource)
	if err := executionErr; err != nil {
		return b.mediaExecutionError(ctx, in, item, err)
	}
	if _, err := b.DB.Exec(
		ctx,
		`UPDATE bot.media_intake SET status='done',notice=$3,model_text='' WHERE owner=$1 AND id=$2`,
		in.owner,
		item.ID,
		string(i18n.MediaSaved),
	); err != nil {
		return core.DatabaseOperationError(err)
	}
	state := interaction.RegistrationMenu{Event: command.Event, View: registrationPayment}
	_, revision, err := b.passMenuState(ctx, in.owner)
	if err != nil {
		return err
	}
	if err = b.storePassMenu(ctx, in.owner, in.chat, revision, state); err != nil {
		return err
	}
	if err = b.RenderPassMenu(ctx, in.owner, in.chat, ""); err != nil {
		return err
	}
	return b.RenderMedia(ctx, in.owner, in.chat, item.ID)
}
