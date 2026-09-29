package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const mediaPrefix = "media:"
const mediaDone = "done"
const mediaChoose = "choose"
const mediaAvatar = "avatar"
const mediaReceipt = "receipt"
const mediaCancel = "cancel"
const mediaOrderChoice = "order"
const mediaForbidden = "forbidden"

type mediaIntake struct {
	CommandSource       *readsource.Derivation
	CommandOrigin       string
	ID                  string
	AttachmentID        string
	Status              string
	Notice              string
	Text                string
	Command             *orders.Command
	RegistrationCommand *passbooking.Command
	FoodCommand         *legacyfood.Command
}

func (b *Bot) loadMediaIntake(ctx context.Context, owner, id string) (mediaIntake, error) {
	var item mediaIntake
	err := b.DB.QueryRow(ctx, `SELECT id,attachment_id,status,notice,model_text,command,registration_command,food_command,command_source,last_origin FROM bot.media_intake
WHERE owner=$1 AND id=$2 AND expires_at>now()`, owner, id).
		Scan(&item.ID, &item.AttachmentID, &item.Status, &item.Notice, &item.Text, &item.Command, &item.RegistrationCommand, &item.FoodCommand, &item.CommandSource, &item.CommandOrigin)
	return item, err
}

// Completed notices outlive neutral attachment storage; this never loads an actionable intake.
func (b *Bot) loadMediaOutcome(ctx context.Context, owner, id string) (mediaIntake, error) {
	var item mediaIntake
	err := b.DB.QueryRow(ctx, `SELECT id,attachment_id,status,notice,model_text,command,registration_command,food_command,command_source,last_origin FROM bot.media_intake
WHERE owner=$1 AND id=$2 AND status='done'`, owner, id).
		Scan(&item.ID, &item.AttachmentID, &item.Status, &item.Notice, &item.Text, &item.Command, &item.RegistrationCommand, &item.FoodCommand, &item.CommandSource, &item.CommandOrigin)
	return item, err
}

// Upload retries inspect durable effects before deciding whether an expired source is needed.
func (b *Bot) loadMediaUploadState(ctx context.Context, owner, id string) (mediaIntake, bool, error) {
	var item mediaIntake
	var expired bool
	err := b.DB.QueryRow(ctx, `SELECT id,attachment_id,status,notice,model_text,command,registration_command,food_command,command_source,last_origin,expires_at<=now()
FROM bot.media_intake WHERE owner=$1 AND id=$2`, owner, id).
		Scan(&item.ID, &item.AttachmentID, &item.Status, &item.Notice, &item.Text, &item.Command, &item.RegistrationCommand, &item.FoodCommand, &item.CommandSource, &item.CommandOrigin, &expired)
	return item, expired, err
}

func (b *Bot) handleMediaUpload(ctx context.Context, in incoming, update telegram.Update) error {
	in.mediaID = "tg-media-" + strconv.FormatInt(update.ID, 10)
	in.text, in.origin = update.Message.Caption, originAgent
	item, expired, err := b.loadMediaUploadState(ctx, in.owner, in.mediaID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = b.saveMediaUpload(ctx, in, update)
	}
	if err != nil {
		if terminalMediaUploadFailure(err) {
			return b.mediaNotice(ctx, in, i18n.MediaUnavailable)
		}
		return err
	}
	if handled, resumeErr := b.resumeConsumedVoice(ctx, in, update.ID, item.Status); handled || resumeErr != nil {
		return resumeErr
	}
	if handled, resumeErr := b.resumeMediaIntake(ctx, in, item, expired); handled {
		return resumeErr
	}
	if handled, avErr := b.prepareAV(ctx, in); handled || avErr != nil {
		return avErr
	}
	err = b.handleAgentUpdate(ctx, in, update.ID)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok &&
		(problem.Code == "media_not_found" || problem.Code == mediaForbidden) && problem.Status < http.StatusInternalServerError {
		return b.retireMediaView(ctx, mediaView{ID: in.mediaID, Owner: in.owner, Chat: in.chat})
	}
	return err
}

// Every retry path resolves durable outcomes before source expiry or availability.
// Expiry blocks new selections, not replay of an already selected proof command.
func (b *Bot) resumeMediaIntake(ctx context.Context, in incoming, item mediaIntake, expired bool) (bool, error) {
	if item.Status == mediaDone {
		return true, b.RenderMedia(ctx, in.owner, in.chat, item.ID)
	}
	if item.FoodCommand != nil && (!expired || item.FoodCommand.ProofID != "") {
		return true, b.commitFoodReceipt(ctx, in, item)
	}
	if item.Command != nil && (!expired || item.Command.ProofFile != "") {
		return true, b.commitMediaReceipt(ctx, in, item)
	}
	if item.RegistrationCommand != nil && (!expired || item.RegistrationCommand.ProofID != "") {
		return true, b.commitRegistrationReceipt(ctx, in, item)
	}
	if expired {
		return true, b.retireMediaView(ctx, mediaView{ID: item.ID, Owner: in.owner, Chat: in.chat})
	}
	return false, nil
}

func terminalMediaUploadFailure(err error) bool {
	if errors.Is(err, telegram.ErrInvalidDocument) || errors.Is(err, telegram.ErrAVAttachment) {
		return true
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok {
		return problem.Status < http.StatusInternalServerError &&
			(problem.Code == "invalid_media" || problem.Code == mediaForbidden)
	}
	if problem, ok := errors.AsType[*telegram.APIError](err); ok {
		return problem.Code == http.StatusBadRequest || problem.Code == http.StatusForbidden ||
			problem.Code == http.StatusNotFound
	}
	return false
}

func (b *Bot) saveMediaUpload(ctx context.Context, in incoming, update telegram.Update) error {
	var document telegram.Document
	av, present, avErr := telegram.SelectAV(*update.Message)
	if avErr != nil {
		return avErr
	}
	switch {
	case present:
		document = av.Document
	case update.Message.Document != nil:
		document = *update.Message.Document
	default:
		var err error
		document, err = telegram.PhotoDocument(update.Message.Photo)
		if err != nil {
			return err
		}
	}
	body, err := b.TG.Download(ctx, document)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(document.Filename)
	if name == "" {
		name = "attachment"
	}
	attachment, err := b.API.UploadMedia(ctx, in.owner, name, body)
	if err != nil {
		return err
	}
	_, err = b.DB.Exec(ctx, `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,av_kind)
VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, in.mediaID, in.owner, update.ID, attachment.ID, string(av.Kind))
	return err
}

func (b *Bot) mediaNotice(ctx context.Context, in incoming, id i18n.ID) error {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	text, err := i18n.Translate(preference.Language, id, nil)
	if err != nil {
		return err
	}
	return b.deliverOrderCard(ctx, in.owner, mediaPrefix+"notice", telegram.Send{ChatID: in.chat, Text: text})
}

func (b *Bot) addMediaContext(ctx context.Context, in incoming, input *agent.Input) error {
	// Keep explicit references and displayed receipt choices ahead of unrelated
	// unfinished media. All candidates still belong to the authenticated owner.
	rows, err := b.DB.Query(ctx, `SELECT id,status FROM bot.media_intake WHERE owner=$1
AND status<>'done' AND expires_at>now()
ORDER BY (id=$2) DESC,(strpos($3,id)>0) DESC,(status='choose') DESC,update_id DESC LIMIT 10`,
		in.owner, in.mediaID, agenthost.CurrentRequestEvidence(*input))
	if err != nil {
		return err
	}
	type pendingHint struct {
		ID   string
		Kind string
	}
	pending, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pendingHint])
	if err != nil {
		return err
	}
	hints := make([]agent.MediaHint, 0, len(pending))
	for _, item := range pending {
		hint, hintErr := b.visibleMediaHint(ctx, in.owner, item.ID, item.Kind)
		if hintErr != nil {
			return hintErr
		}
		hints = append(hints, hint)
	}
	input.MediaContext = &agent.MediaContext{Pending: hints}
	if err = b.addKnownReceipt(ctx, in.owner, input); err != nil {
		return err
	}
	input.MediaContext.Recent, err = b.mediaRecent(ctx, in.owner)
	if err != nil {
		return err
	}
	if in.mediaID != "" {
		if err = b.addCurrentMedia(ctx, in, input); err != nil {
			return err
		}
	}
	if len(hints) == 0 {
		return nil
	}
	candidates, err := b.mediaCandidates(ctx, in.owner)
	if err != nil {
		return err
	}
	input.MediaContext.Candidates = candidates
	var selected orders.Command
	err = b.DB.QueryRow(ctx, `SELECT command FROM bot.proof_pending WHERE owner=$1`, in.owner).Scan(&selected)
	if err == nil {
		for _, candidate := range candidates {
			if candidate.OrderID == selected.OrderID && candidate.Version == selected.Version {
				input.MediaContext.SelectedOrderID = selected.OrderID
			}
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (b *Bot) addCurrentMedia(ctx context.Context, in incoming, input *agent.Input) error {
	if input.AV != nil {
		return nil
	}
	item, err := b.loadMediaIntake(ctx, in.owner, in.mediaID)
	if err != nil {
		return err
	}
	attachment, err := b.API.Media(ctx, in.owner, item.AttachmentID)
	if err != nil {
		return err
	}
	if handled, avErr := b.addAVInput(ctx, in.owner, in.mediaID, input); handled || avErr != nil {
		return avErr
	}
	if attachment.MIME == "image/png" || attachment.MIME == "image/jpeg" {
		input.Attachment = &agent.Attachment{
			ID:       in.mediaID,
			Filename: attachment.Filename,
			MIME:     attachment.MIME,
			Body:     attachment.Body,
		}
	} else {
		input.Text = fmt.Sprintf(
			"[Current attachment %s has unsupported visual format %s; bytes were not interpreted.]\n%s",
			in.mediaID,
			attachment.MIME,
			input.Text,
		)
	}
	input.View = agent.MediaView
	return nil
}

func (b *Bot) mediaCandidates(ctx context.Context, owner string) ([]agent.MediaCandidate, error) {
	list, err := b.API.Orders(ctx, owner, b.currentOrderEvent())
	if err != nil {
		return nil, err
	}
	var pending orders.Command
	pendingErr := b.DB.QueryRow(ctx, `SELECT command FROM bot.proof_pending WHERE owner=$1`, owner).Scan(&pending)
	if pendingErr != nil && !errors.Is(pendingErr, pgx.ErrNoRows) {
		return nil, pendingErr
	}
	if pendingErr == nil && pending.EventID != b.currentOrderEvent() {
		selected, selectedErr := b.API.Order(ctx, owner, pending.EventID, pending.OrderID)
		if selectedErr == nil {
			list = append(list, selected)
		} else if problem, ok := errors.AsType[*core.ProblemError](selectedErr); !ok || problem.Status >= http.StatusInternalServerError {
			return nil, selectedErr
		}
	}
	var candidates []agent.MediaCandidate
	for _, order := range list {
		if order.State != stateUnpaid && order.State != stateCash {
			continue
		}
		payment, paymentErr := b.API.PaymentInstructions(ctx, owner, order.EventID, order.ID)
		if paymentErr != nil {
			return nil, paymentErr
		}
		if !payment.CanPay {
			continue
		}
		cents := int64(payment.TotalBYN)
		const centsPerUnit = 100
		candidates = append(candidates, agent.MediaCandidate{OrderID: order.ID, Version: order.Version,
			AmountBYN: fmt.Sprintf("%d.%02d", cents/centsPerUnit, cents%centsPerUnit), AmountRUB: payment.TotalRUB})
	}
	registration, err := b.registrationMediaCandidates(ctx, owner)
	return append(candidates, registration...), err
}
