package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type passMenuChoice struct {
	label, url string
	action     passMenuAction
}
type passMenuRenderer struct {
	bot             *Bot
	owner, language string
	state           interaction.RegistrationMenu
	lines           []string
	choices         []passMenuChoice
}

func (r *passMenuRenderer) text(id i18n.ID) string {
	value, _ := i18n.Translate(r.language, id, nil)
	return value
}
func (r *passMenuRenderer) navigate(id i18n.ID, view string) {
	state := interaction.RegistrationMenu{Event: r.state.Event, View: view, PaymentAdmin: r.state.PaymentAdmin}
	state.Historical = r.state.Historical && historicalPassView(view)
	r.choices = append(r.choices, passMenuChoice{label: r.text(id), action: passMenuAction{View: &state}})
}
func passMenuLabel(value string) string { return interaction.RegistrationLabel(value) }

func (b *Bot) RenderPassMenu(ctx context.Context, owner string, chat int64, notice i18n.ID) error {
	saved, revision, err := b.passMenuRecord(ctx, owner)
	if err != nil {
		return err
	}
	if saved.Redacted {
		return b.renderRedactedPassMenu(ctx, owner, chat, revision, saved)
	}
	err = b.renderCurrentPassMenu(ctx, owner, chat, revision, saved, notice)
	if saved.Source != nil && stalePassMenuSource(err) {
		return b.redactPassMenu(ctx, owner, chat, revision, saved)
	}
	return err
}

func (b *Bot) renderCurrentPassMenu(
	ctx context.Context,
	owner string,
	chat, revision int64,
	saved botdelivery.PassMenu,
	notice i18n.ID,
) error {
	var err error
	state, source := saved.RegistrationMenu, saved.Source
	if err = b.checkPassDeliverySource(ctx, owner, source); err != nil {
		return err
	}
	if notice != "" {
		state.Notice = notice
	}
	prefs, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	r := passMenuRenderer{bot: b, owner: owner, language: prefs.Language, state: state}
	if err = r.build(ctx); err != nil {
		if failure := passMenuFailure(err); failure != nil {
			return failure
		}
		r.lines, r.choices = nil, nil
		r.state = interaction.RegistrationMenu{View: passMenuEvents}
		if err = r.events(ctx); err != nil {
			return err
		}
		r.state.Notice = i18n.RegistrationStale
	}
	if len(r.lines) == 0 {
		r.lines = append(r.lines, r.text(i18n.RegistrationEmpty))
	}
	if r.state.View != agent.RegistrationTakeoverTarget {
		if err = r.eventHeading(ctx); err != nil {
			return err
		}
	}
	if r.state.Event != "" {
		r.navigate(i18n.RegistrationHome, passMenuHome)
	}
	if r.state.View != passMenuEvents {
		r.navigate(i18n.RegistrationEvents, passMenuEvents)
	}
	text := r.text(i18n.RegistrationTitle) + "\n\n" + strings.Join(r.lines, "\n\n")
	if r.state.Notice != "" {
		text = r.text(r.state.Notice) + "\n\n" + text
	}
	if err = b.storePassMenu(ctx, owner, chat, revision, r.state); err != nil {
		return err
	}
	payload, err := b.registrationReplyPayload(ctx, owner, revision, telegram.Send{ChatID: chat, Text: text})
	if err != nil {
		return err
	}
	return b.deliverPassMenu(ctx, owner, revision, telegram.FormatSend(payload), r.choices, r.state, source)
}

func (b *Bot) deliverPassMenu(
	ctx context.Context,
	owner string,
	revision int64,
	payload telegram.Send,
	choices []passMenuChoice,
	state interaction.RegistrationMenu,
	source *readsource.Derivation,
) error {
	tokens := []string{}
	for _, choice := range choices {
		button := telegram.Button{Text: choice.label, URL: choice.url}
		if choice.url == "" {
			data, err := json.Marshal(choice.action)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(fmt.Appendf(nil, "pass/%s/%d/%s", owner, revision, data))
			token := hex.EncodeToString(digest[:16])
			if _, err = b.DB.Exec(ctx, `INSERT INTO bot.pass_buttons(owner,token,revision,action) VALUES($1,$2,$3,$4)
			ON CONFLICT(owner,token) DO NOTHING`, owner, token, revision, choice.action); err != nil {
				return core.DatabaseOperationError(err)
			}
			tokens = append(tokens, token)
			button.Data = passMenuPrefix + token
		}
		payload.Markup.Rows = append(payload.Markup.Rows, []telegram.Button{button})
	}
	hash, err := botCardHash(payload)
	if err != nil {
		return err
	}
	var previous string
	if err = b.DB.QueryRow(ctx, "SELECT message_id,view_hash FROM bot.pass_views WHERE owner=$1", owner).
		Scan(&payload.MessageID, &previous); err != nil {
		return core.DatabaseOperationError(err)
	}
	if payload.MessageID > 0 && previous == hash {
		return nil
	}
	ref := botdelivery.Reference{
		Family:   botFamilyPasses,
		CardKey:  botFamilyPasses,
		Revision: revision,
		Source:   source,
		Notice:   state.Notice,
	}
	return b.queueBotCard(
		ctx,
		owner,
		payload,
		ref,
		botdelivery.Continuation{Kind: botPassCardReceipt, Revision: revision, ViewHash: hash, Tokens: tokens},
	)
}
func passMenuEditFallback(err error) (bool, error) {
	var apiError *telegram.APIError
	if !errors.As(err, &apiError) || apiError.Code != http.StatusBadRequest {
		return false, err
	}
	if strings.Contains(apiError.Description, "message is not modified") {
		return false, nil
	}
	if strings.Contains(apiError.Description, "message to edit not found") ||
		strings.Contains(apiError.Description, "message can't be edited") {
		return true, nil
	}
	return false, err
}

func (r *passMenuRenderer) build(ctx context.Context) error {
	if r.state.Historical {
		return r.historicalPass(ctx)
	}
	switch r.state.View {
	case agent.RegistrationTakeoverTarget:
		return r.takeoverTarget(ctx)
	case agent.RegistrationAdminTarget:
		return r.adminAssignment(ctx)
	case passMenuEvents:
		return r.events(ctx)
	case passMenuQueue:
		return r.queue(ctx)
	case passMenuInvitations:
		return r.invitations(ctx)
	case registrationPaymentQueue:
		return r.paymentQueue(ctx)
	case registrationPayment:
		return r.payment(ctx)
	case "admins":
		return r.admins(ctx)
	case profileCardKey:
		return r.profile(ctx)
	case passInvite:
		r.lines = append(r.lines, r.text(i18n.RegistrationInviteHint))
		return nil
	default:
		return r.home(ctx)
	}
}

func (r *passMenuRenderer) events(ctx context.Context) error {
	events, err := r.bot.API.PassEvents(ctx, r.owner)
	if err != nil {
		return err
	}
	first, last := r.page(len(events), "")
	for _, event := range events[first:last] {
		label := strings.TrimSpace(event.CountryEmoji + " " + event.Title(r.language, true))
		state := interaction.RegistrationMenu{Event: event.ID, View: passMenuHome}
		r.choices = append(r.choices, passMenuChoice{label: passMenuLabel(label), action: passMenuAction{View: &state}})
	}
	r.lines = append(r.lines, r.text(i18n.RegistrationEvents))
	r.choices = append(
		r.choices,
		passMenuChoice{label: r.text(i18n.RegistrationExport), action: passMenuAction{Export: true}},
	)
	return nil
}

func (r *passMenuRenderer) home(ctx context.Context) error {
	booking, err := r.bot.API.PassBooking(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	r.lines = append(r.lines, r.bookingText(booking))
	if err = r.paymentContact(ctx, booking); err != nil {
		return err
	}
	r.navigate(i18n.RegistrationProfile, profileCardKey)
	r.navigate(i18n.RegistrationAdmin, "admins")
	if booking.Version == 0 || booking.State == passStateCancelled || booking.State == "waiting-for-couple" ||
		(booking.State == "waitlist" && booking.Partner == "") {
		if err = r.registrationChoices(ctx, booking); err != nil {
			return err
		}
	}
	r.navigate(i18n.RegistrationInvitations, passMenuInvitations)
	if err = r.paymentNavigation(ctx, booking); err != nil {
		return err
	}
	if booking.Version > 0 && booking.State != passStateCancelled && booking.State != "paid" {
		r.command(
			i18n.RegistrationCancel,
			passbooking.Command{Name: mediaCancel, Event: r.state.Event, Version: booking.Version},
		)
	}
	if _, err = r.bot.API.PassQueue(ctx, r.owner, r.state.Event, ""); err == nil {
		r.takeoverLink(0, "")
		r.navigate(i18n.RegistrationAssignment, agent.RegistrationAdminTarget)
		r.navigate(i18n.RegistrationQueue, passMenuQueue)
		r.command(
			i18n.RegistrationRecalculate,
			passbooking.Command{Name: "recalculate", Event: r.state.Event, Version: booking.Version},
		)
	}
	return passMenuFailure(err)
}

func (r *passMenuRenderer) command(id i18n.ID, command passbooking.Command) {
	r.choices = append(r.choices, passMenuChoice{label: r.text(id), action: passMenuAction{Command: &command}})
}

func (r *passMenuRenderer) bookingText(booking passbooking.Booking) string {
	id := i18n.RegistrationNoBooking
	switch booking.State {
	case "waiting-for-couple":
		id = i18n.RegistrationWaitingPartner
	case "waitlist":
		id = i18n.RegistrationWaiting
	case registrationAssigned:
		id = i18n.RegistrationAssigned
	case statePaid:
		id = i18n.RegistrationPaid
	case passStateCancelled:
		id = i18n.RegistrationCancelled
	}
	text, _ := i18n.Translate(r.language, i18n.RegistrationStatus, map[string]string{"status": r.text(id)})
	if booking.Price != nil {
		price, _ := i18n.Translate(
			r.language,
			i18n.RegistrationPrice,
			map[string]string{passPriceParameter: strconv.Itoa(*booking.Price)},
		)
		text += "\n" + price
	}
	return text
}

func (r *passMenuRenderer) profile(ctx context.Context) error {
	profile, err := r.bot.API.PassProfile(ctx, r.owner)
	if err != nil {
		return err
	}
	text, err := profileText(r.language, profile, "")
	if err != nil {
		return err
	}
	r.lines = append(r.lines, text)
	if profile.Passport == "" {
		r.lines = append(r.lines, r.text(i18n.RegistrationIdentityDocumentMissing))
	}
	if !profile.Frozen {
		r.profileCommand(
			i18n.ProfileSetName,
			passes.Command{Name: profileBegin, Field: profileLegalName, Version: profile.Version, Origin: originManual},
		)
		r.profileCommand(
			i18n.RegistrationIdentityDocument,
			passes.Command{
				Name:    profileBegin,
				Field:   profilePassportField,
				Version: profile.Version,
				Origin:  originManual,
			},
		)
	}
	r.profileCommand(
		i18n.RegistrationLeader,
		passes.Command{
			Name:    profileSet,
			Field:   profileRoleField,
			Value:   profileLeader,
			Version: profile.Version,
			Origin:  originManual,
		},
	)
	r.profileCommand(
		i18n.RegistrationFollower,
		passes.Command{
			Name:    profileSet,
			Field:   profileRoleField,
			Value:   profileFollower,
			Version: profile.Version,
			Origin:  originManual,
		},
	)
	return nil
}

func (r *passMenuRenderer) profileCommand(id i18n.ID, command passes.Command) {
	r.choices = append(r.choices, passMenuChoice{label: r.text(id), action: passMenuAction{Profile: &command}})
}
