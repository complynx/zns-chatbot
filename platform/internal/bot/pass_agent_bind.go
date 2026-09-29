package bot

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func bindRegistrationPlan(plan agent.Plan, input agent.Input) (*passbooking.Command, *passMenuState, error) {
	p := plan.RegistrationAction
	if p == nil {
		return nil, nil, nil
	}
	context := input.Registration
	if context == nil || !registrationEventKnown(context, p.Event) {
		return nil, nil, errors.New("registration event lacks evidence")
	}
	view := p.View
	if view == "" {
		view = passMenuHome
	}
	state := &passMenuState{Event: p.Event, View: view}
	if isTakeoverProposal(*p) {
		return bindTakeover(*p, context)
	}
	if p.Name == agent.RegistrationShow {
		return nil, state, bindAdminTargetView(context, *p, state)
	}
	if p.Name == agent.RegistrationRead {
		return nil, nil, errors.New("registration read was not consumed")
	}
	booking := registrationOwn(context, p.Event)
	if booking == nil {
		return nil, nil, errors.New("registration requires current read")
	}
	command := &passbooking.Command{
		Name:         p.Name,
		Event:        p.Event,
		Version:      booking.Version,
		PaymentAdmin: p.PaymentAdmin,
	}
	if p.PaymentAdmin != "" && !registrationAdminKnown(context, p.Event, p.PaymentAdmin) {
		return nil, nil, errors.New("payment contact lacks evidence")
	}
	switch p.Name {
	case registrationProofAccept, registrationProofReject:
		payment := registrationReviewEvidence(context, *p)
		if payment == nil {
			return nil, nil, errors.New("payment review lacks evidence")
		}
		command.Target, command.TargetVersion, command.PaymentAttempt = p.Target, payment.Version, payment.Attempt
		state.View = registrationPaymentQueue
	case passInvite:
		if !groundedRegistrationContact(input, p.InviteTelegramID) &&
			!registrationQueueContact(input, p.Event, p.InviteTelegramID) {
			return nil, nil, errors.New("registration contact lacks evidence")
		}
		command.InviteTelegramID = p.InviteTelegramID
		command.QueueInvitation = !groundedRegistrationContact(input, p.InviteTelegramID)
	case passAccept, passDecline, "admin_cancel", "admin_uncouple":
		version, found := registrationTarget(context, *p)
		if !found {
			return nil, nil, errors.New("registration target lacks evidence")
		}
		command.Target, command.TargetVersion = p.Target, version
	case "recalculate":
		if !registrationQueueRead(context, p.Event) {
			return nil, nil, errors.New("administrator read required")
		}
	}
	return command, state, nil
}

// Queue evidence belongs to the authenticated host input and the requested
// event. A contact seen in another event is not invitation evidence here.
func registrationQueueContact(input agent.Input, event string, id int64) bool {
	if input.Registration == nil || id <= 0 {
		return false
	}
	for _, read := range input.Registration.Reads {
		if read.Error != "" || read.Omitted || read.Request.Event != event || read.Request.View != passMenuQueue {
			continue
		}
		for _, booking := range read.Queue {
			if booking.Event == event && booking.TelegramID == id {
				return true
			}
		}
	}
	return false
}

func registrationEventKnown(context *agent.RegistrationContext, event string) bool {
	if event == "" {
		return true
	}
	for _, item := range context.Events {
		if item.ID == event {
			return true
		}
	}
	for _, read := range context.Reads {
		if read.Error != "" {
			continue
		}
		if read.TakeoverTarget != nil && read.TakeoverTarget.Booking.Event == event {
			return true
		}
		if read.Booking != nil && read.Booking.Event == event {
			return true
		}
		for _, item := range read.Events {
			if item.ID == event {
				return true
			}
		}
	}
	return false
}

func registrationOwn(context *agent.RegistrationContext, event string) *passbooking.Booking {
	for _, read := range slices.Backward(context.Reads) {
		if read.Error == "" && read.Booking != nil && read.Booking.Event == event {
			return read.Booking
		}
	}
	return nil
}

func registrationAdminKnown(context *agent.RegistrationContext, event, owner string) bool {
	for _, read := range context.Reads {
		if read.Request.Event != event || read.Error != "" {
			continue
		}
		for _, admin := range read.PaymentAdmins {
			if admin.Owner == owner {
				return true
			}
		}
	}
	return false
}

func registrationQueueRead(context *agent.RegistrationContext, event string) bool {
	for _, read := range context.Reads {
		if read.Request.Event == event && read.Request.View == passMenuQueue && read.Error == "" {
			return true
		}
	}
	return false
}

func registrationTarget(context *agent.RegistrationContext, p agent.RegistrationProposal) (int64, bool) {
	for _, read := range slices.Backward(context.Reads) {
		if read.Error != "" || read.Request.Event != p.Event {
			continue
		}
		if p.Name == passAccept || p.Name == passDecline {
			for _, invite := range read.Invitations {
				if invite.From.Owner == p.Target {
					return invite.Version, true
				}
			}
		} else {
			for _, booking := range read.Queue {
				if booking.Owner == p.Target {
					return booking.Version, true
				}
			}
		}
	}
	return 0, false
}

func groundedRegistrationContact(input agent.Input, id int64) bool {
	if id <= 0 || input.Registration == nil {
		return false
	}
	if slices.Contains(input.Registration.TrustedPartnerIDs, id) {
		return true
	}
	needle := strconv.FormatInt(id, 10)
	tokens := strings.FieldsFunc(currentRequestEvidence(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '@'
	})
	return slices.Contains(tokens, needle)
}
