package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) executeMediaPlan(ctx context.Context, in incoming, updateID int64, cached interaction.SavedPlan) error {
	if err := b.planAuthorization().ValidatePlan(ctx, in.owner, updateID, cached); err != nil {
		return err
	}
	id := cached.MediaID
	proposal := cached.Plan.MediaAction
	if proposal != nil {
		id = proposal.MediaID
	}
	if id == "" {
		return b.deliverOrderCard(
			ctx,
			in.owner,
			mediaPrefix+"notice",
			telegram.FormatSend(telegram.Send{ChatID: in.chat, Text: cached.Plan.Text, NativeMarkdown: true}),
		)
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
	status, notice := "purpose", i18n.MediaPurpose
	if proposal == nil && item.Status != "new" {
		status, notice = item.Status, i18n.ID(item.Notice)
	}
	if proposal != nil {
		switch proposal.Intent {
		case mediaAvatar:
			status, notice = mediaDone, i18n.MediaAvatarUnavailable
		case mediaCancel:
			status, notice = mediaDone, i18n.MediaClosed
		case mediaReceipt:
			if handled, receiptErr := b.selectMediaReceipt(ctx, in, item, cached); handled {
				return receiptErr
			}
			status, notice = mediaChoose, i18n.MediaChoose
		case "inspect_video":
			notice = i18n.MediaUnsupported
		}
	}
	action := "answer"
	if proposal != nil {
		action = proposal.Intent
	}
	err = b.storeMediaModelReply(ctx, in.owner, id, status, string(notice), cached.Plan.Text, action, updateID)
	if err != nil {
		return err
	}
	return b.RenderMedia(ctx, in.owner, in.chat, id)
}

// A price match must be unique across the full authorized snapshot. A model's
// order ID alone is not explicit user selection.
func mediaMatch(p agent.MediaProposal, candidates []agent.MediaCandidate, selected, text string) *agent.MediaCandidate {
	if candidate := explicitRegistrationCandidate(p, candidates, text); candidate != nil {
		return candidate
	}
	if p.RegistrationEvent != "" {
		return nil
	}
	if p.OrderID != "" &&
		(strings.Contains(text, p.OrderID) || (p.OrderID == selected && p.Amount == "" && p.Currency == "")) {
		return mediaCandidateByID(candidates, p.OrderID)
	}
	if selected != "" && p.RegistrationEvent == "" && p.Amount == "" && p.Currency == "" {
		return mediaCandidateByID(candidates, selected)
	}
	amount, ok := mediaAmountCents(p.Amount)
	if !ok || (p.Currency != "BYN" && p.Currency != "RUB") {
		return nil
	}
	var match *agent.MediaCandidate
	for _, candidate := range candidates {
		price := candidate.AmountBYN
		if p.Currency == "RUB" {
			price = candidate.AmountRUB
		}
		value, valid := mediaAmountCents(price)
		if valid && amount == value {
			if match != nil {
				return nil
			}
			copyCandidate := candidate
			match = &copyCandidate
		}
	}
	return match
}

func explicitRegistrationCandidate(
	p agent.MediaProposal,
	candidates []agent.MediaCandidate,
	text string,
) *agent.MediaCandidate {
	if p.RegistrationEvent == "" {
		return nil
	}
	for _, candidate := range candidates {
		if candidate.RegistrationEvent == p.RegistrationEvent && registrationNamed(candidate, text) {
			return &candidate
		}
	}
	return nil
}

func registrationNamed(candidate agent.MediaCandidate, text string) bool {
	text = strings.ToLower(text)
	if strings.Contains(text, strings.ToLower(candidate.RegistrationEvent)) {
		return true
	}
	for _, title := range candidate.RegistrationTitles {
		if title != "" && strings.Contains(text, strings.ToLower(title)) {
			return true
		}
	}
	return false
}

func (b *Bot) selectMediaReceipt(
	ctx context.Context,
	in incoming,
	item mediaIntake,
	cached interaction.SavedPlan,
) (bool, error) {
	source, err := savedPlanSource(cached)
	if err != nil {
		return true, err
	}
	if target := cached.MediaResolvedFood; target != nil {
		return true, b.chooseAgentFoodReceipt(ctx, in, item.ID, *target, source)
	}
	if cached.Plan.MediaAction.FoodKind != "" {
		return false, nil
	}
	candidate := mediaMatch(*cached.Plan.MediaAction, cached.MediaCandidates, cached.MediaSelected, in.text)
	if cached.MediaResolvedRegistration != "" {
		candidate = &agent.MediaCandidate{
			RegistrationEvent: cached.MediaResolvedRegistration,
			Version:           cached.MediaResolvedVersion,
		}
	} else if cached.MediaResolvedOrder != "" {
		candidate = &agent.MediaCandidate{OrderID: cached.MediaResolvedOrder, Version: cached.MediaResolvedVersion}
	}
	if candidate == nil {
		return false, nil
	}
	if candidate.RegistrationEvent != "" {
		return true, b.chooseRegistrationReceiptWithSource(ctx, in, item, *candidate, originAgent, &source)
	}
	return true, b.chooseMediaReceiptWithSource(ctx, in, item, *candidate, originAgent, &source)
}

func mediaCandidateByID(candidates []agent.MediaCandidate, id string) *agent.MediaCandidate {
	for _, candidate := range candidates {
		if candidate.OrderID == id {
			return &candidate
		}
	}
	return nil
}

func mediaAmountCents(value string) (int64, bool) {
	whole, fraction, _ := strings.Cut(value, ".")
	if len(whole) == 0 || len(whole) > 9 || len(fraction) > 2 {
		return 0, false
	}
	for _, r := range whole + fraction {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	const decimalPlaces = 2
	fraction += strings.Repeat("0", decimalPlaces-len(fraction))
	amount, err := strconv.ParseInt(whole+fraction, 10, 64)
	return amount, err == nil && amount > 0
}

func (b *Bot) chooseMediaReceipt(
	ctx context.Context,
	in incoming,
	item mediaIntake,
	candidate agent.MediaCandidate,
	origin string,
) error {
	return b.chooseMediaReceiptWithSource(ctx, in, item, candidate, origin, nil)
}

func (b *Bot) chooseMediaReceiptWithSource(ctx context.Context, in incoming, item mediaIntake,
	candidate agent.MediaCandidate, origin string, source *readsource.Derivation) error {
	boundSource, sourceErr := mediaCommandSource(origin, source)
	if sourceErr != nil {
		return sourceErr
	}
	if source != nil {
		source = &boundSource
	}
	event, resolveErr := b.OrderEventForOrder(ctx, in.owner, candidate.OrderID)
	if resolveErr != nil {
		return b.mediaExecutionError(ctx, in, item, resolveErr)
	}
	command := orders.Command{Name: stateProof, EventID: event, OrderID: candidate.OrderID,
		Version: candidate.Version, Origin: origin, Key: item.ID}
	// First choice wins and is persisted before any external effect.
	err := b.DB.QueryRow(ctx, `UPDATE bot.media_intake SET command=$3,last_action='select_order',last_origin=$4,command_source=$5 WHERE owner=$1 AND id=$2
AND command IS NULL AND registration_command IS NULL AND food_command IS NULL AND status<>'done' AND expires_at>now() RETURNING command,command_source,last_origin`, in.owner, item.ID, command, origin, source).
		Scan(&item.Command, &item.CommandSource, &item.CommandOrigin)
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = b.loadMediaIntake(ctx, in.owner, item.ID)
	}
	if err != nil {
		return err
	}
	if item.Command == nil {
		return b.RenderMedia(ctx, in.owner, in.chat, item.ID)
	}
	return b.commitMediaReceipt(ctx, in, item)
}

func (b *Bot) commitMediaReceipt(ctx context.Context, in incoming, item mediaIntake) error {
	if _, err := mediaCommandSource(item.CommandOrigin, item.CommandSource); err != nil {
		return b.mediaExecutionError(ctx, in, item, err)
	}
	command := *item.Command
	if command.ProofFile == "" {
		proof, err := b.API.PromoteMedia(ctx, in.owner, item.AttachmentID)
		if err != nil {
			return b.mediaExecutionError(ctx, in, item, err)
		}
		command.ProofFile = proof.ID
		encoded, encodeErr := json.Marshal(command)
		if encodeErr != nil {
			return encodeErr
		}
		_, err = b.DB.Exec(
			ctx,
			`UPDATE bot.media_intake SET command=$3 WHERE owner=$1 AND id=$2`,
			in.owner,
			item.ID,
			encoded,
		)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	item.Command = &command
	var err error
	if item.CommandSource == nil {
		_, err = b.API.ExecuteOrder(ctx, in.owner, command)
	} else {
		_, err = b.Host.ExecuteDerivedOrder(ctx, in.owner, command, *item.CommandSource)
	}
	if err != nil {
		return b.mediaExecutionError(ctx, in, item, err)
	}
	_, err = b.DB.Exec(
		ctx,
		`UPDATE bot.media_intake SET status='done',notice=$3,model_text='' WHERE owner=$1 AND id=$2`,
		in.owner,
		item.ID,
		string(i18n.MediaSaved),
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if err = b.RenderOrders(ctx, in.owner, in.chat); err != nil {
		return err
	}
	return b.RenderMedia(ctx, in.owner, in.chat, item.ID)
}

func (b *Bot) mediaExecutionError(ctx context.Context, in incoming, item mediaIntake, err error) error {
	if core.IsDatabaseFailure(err) {
		return err
	}
	problem, ok := errors.AsType[*core.ProblemError](err)
	if !ok || problem.Status >= http.StatusInternalServerError {
		return err
	}
	notice := i18n.MediaStale
	// Authorization runs before Core's replay lookup. A denied retry cannot tell
	// whether a previously dispatched proof command committed successfully.
	if ((item.Command != nil && item.Command.ProofFile != "") ||
		(item.RegistrationCommand != nil && item.RegistrationCommand.ProofID != "") ||
		(item.FoodCommand != nil && item.FoodCommand.ProofID != "")) &&
		(problem.Status == http.StatusUnauthorized || problem.Status == http.StatusForbidden ||
			problem.Code == "event_not_found") {
		notice = i18n.MediaOutcomeUnknown
	}
	_, err = b.DB.Exec(
		ctx,
		`UPDATE bot.media_intake SET status='done',notice=$3,model_text='' WHERE owner=$1 AND id=$2`,
		in.owner,
		item.ID,
		string(notice),
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return b.RenderMedia(ctx, in.owner, in.chat, item.ID)
}
