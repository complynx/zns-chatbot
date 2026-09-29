package bot

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (r *passMenuRenderer) takeoverLink(id int64, label string) {
	state := passMenuState{Event: r.state.Event, View: agent.RegistrationTakeoverTarget, AdminTargetTelegramID: id}
	for _, choice := range r.choices {
		if view := choice.action.View; view != nil && view.View == state.View && view.AdminTargetTelegramID == id {
			return
		}
	}
	r.choices = append(
		r.choices,
		passMenuChoice{label: r.text(i18n.RegistrationTakeover) + label, action: passMenuAction{View: &state}},
	)
}

func (r *passMenuRenderer) takeoverTarget(ctx context.Context) error {
	if r.state.AdminTargetTelegramID == 0 {
		return r.takeoverHint(ctx)
	}
	target, err := r.bot.API.PassTakeoverTarget(ctx, r.owner, r.state.Event, r.state.AdminTargetTelegramID)
	if err != nil {
		return err
	}
	r.lines = append(
		r.lines,
		passMenuLabel(target.Name),
		r.bookingText(target.Booking),
		r.text(i18n.RegistrationTakeoverHint),
	)
	text, err := i18n.Translate(
		r.language,
		i18n.RegistrationTakeoverContacts,
		map[string]string{
			"contact":  r.takeoverContact(target.PaymentContact),
			"receiver": r.takeoverContact(target.ReceiverContact),
		},
	)
	if err != nil {
		return err
	}
	r.lines = append(r.lines, text)
	if title := target.EventTitles[r.language]; title != "" {
		r.lines = append([]string{passMenuLabel(title)}, r.lines...)
	} else {
		r.lines = append([]string{passMenuLabel(target.Booking.Event)}, r.lines...)
	}
	command := passbooking.Command{
		Name:          passbooking.CommandTakeover,
		Event:         target.Booking.Event,
		Version:       target.ActorVersion,
		Target:        target.Booking.Owner,
		TargetVersion: target.Booking.Version,
	}
	if target.Booking.PaymentAdmin != r.owner || target.Booking.Partner != "" {
		r.command(i18n.RegistrationTakeoverApply, command)
	}
	if target.CanBackfill {
		command.Name = passbooking.CommandReceivedOnly
		r.command(i18n.RegistrationReceiverApply, command)
	}
	return nil
}

func (r *passMenuRenderer) takeoverHint(ctx context.Context) error {
	_, err := r.bot.API.PassQueue(ctx, r.owner, r.state.Event, "")
	if err != nil {
		if passMenuFailure(err) != nil {
			return err
		}
		if _, err = r.bot.API.PassPaymentQueue(ctx, r.owner, r.state.Event, ""); err != nil {
			return err
		}
	}
	r.lines = append(r.lines, r.text(i18n.RegistrationTakeoverHint))
	return nil
}

func isTakeoverProposal(p agent.RegistrationProposal) bool {
	return p.Name == passbooking.CommandTakeover || p.Name == passbooking.CommandReceivedOnly ||
		(p.Name == agent.RegistrationShow && p.View == agent.RegistrationTakeoverTarget)
}

func takeoverEvidence(value *agent.RegistrationContext, event, owner string) *passbooking.TakeoverTarget {
	for _, read := range slices.Backward(value.Reads) {
		if read.Error == "" && read.Request.Event == event && read.TakeoverTarget != nil &&
			read.TakeoverTarget.Booking.Owner == owner {
			return read.TakeoverTarget
		}
	}
	return nil
}

func bindTakeover(
	p agent.RegistrationProposal,
	value *agent.RegistrationContext,
) (*passbooking.Command, *passMenuState, error) {
	target := takeoverEvidence(value, p.Event, p.Target)
	if target == nil {
		return nil, nil, errors.New("takeover target lacks evidence")
	}
	state := &passMenuState{
		Event:                 p.Event,
		View:                  agent.RegistrationTakeoverTarget,
		AdminTargetTelegramID: target.Booking.TelegramID,
	}
	if p.Name == agent.RegistrationShow {
		return nil, state, nil
	}
	command := &passbooking.Command{
		Name:          p.Name,
		Event:         p.Event,
		Version:       target.ActorVersion,
		Target:        p.Target,
		TargetVersion: target.Booking.Version,
	}
	return command, state, nil
}

func (b *Bot) fetchTakeoverTarget(
	ctx context.Context,
	owner string,
	p agent.RegistrationProposal,
	result agent.RegistrationReadResult,
) (agent.RegistrationReadResult, error) {
	id, err := strconv.ParseInt(p.Target, 10, 64)
	if err != nil {
		return result, err
	}
	target, err := b.API.PassTakeoverTarget(ctx, owner, p.Event, id)
	target.Name = passMenuLabel(target.Name)
	result.TakeoverTarget = &target
	return result, err
}

func (r *passMenuRenderer) takeoverContact(contact *passbooking.Contact) string {
	if contact == nil {
		return r.text(i18n.RegistrationReceiverMissing)
	}
	r.contact(*contact)
	return passMenuLabel(contact.Name)
}

func takeoverEventGrounded(input agent.Input, event string) bool {
	if registrationEventKnown(input.Registration, event) || input.Registration.CurrentEvent == event {
		return true
	}
	tokens := strings.FieldsFunc(currentRequestEvidence(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})
	return event != "" && slices.Contains(tokens, event)
}
