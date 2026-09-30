package interaction

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// BindRegistrationPlan uses admitted host reads and explicit current-request
// evidence. The caller supplies evidence; this package does not parse transport
// messages or consult the execution ledger.
func BindRegistrationPlan(requestEvidence string,
	plan agent.Plan,
	input agent.Input,
) (*passbooking.Command, *RegistrationMenu, error) {
	p := plan.RegistrationAction
	if p == nil {
		return nil, nil, nil
	}
	context := input.Registration
	if !registrationPlanEventKnown(context, *p) {
		return nil, nil, errors.New("registration event lacks evidence")
	}
	view := p.View
	if view == "" {
		view = "home"
	}
	state := &RegistrationMenu{Event: p.Event, View: view}
	if isTakeoverProposal(*p) {
		return bindTakeover(*p, context)
	}
	if p.Name == agent.RegistrationShow {
		state.Historical = RegistrationHistoricalEventKnown(context, p.Event)
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
	case "proof_accept", "proof_reject":
		payment := registrationReviewEvidence(context, *p)
		if payment == nil {
			return nil, nil, errors.New("payment review lacks evidence")
		}
		bound := RegistrationPaymentReviewCommand(p.Name, p.Event, booking.Version, p.Target, *payment)
		bound.PaymentAdmin = command.PaymentAdmin
		*command = bound
		state.View = "payment_queue"
	case "invite":
		if !GroundedRegistrationContact(requestEvidence, input, p.InviteTelegramID) &&
			!registrationQueueContact(input, p.Event, p.InviteTelegramID) {
			return nil, nil, errors.New("registration contact lacks evidence")
		}
		command.InviteTelegramID = p.InviteTelegramID
		command.QueueInvitation = !GroundedRegistrationContact(requestEvidence, input, p.InviteTelegramID)
	case "accept", "decline", "admin_cancel", "admin_uncouple":
		bound, found := registrationTargetCommand(context, *p, booking.Version)
		if !found {
			return nil, nil, errors.New("registration target lacks evidence")
		}
		bound.PaymentAdmin = command.PaymentAdmin
		*command = bound
	case "recalculate":
		if !registrationQueueRead(context, p.Event) {
			return nil, nil, errors.New("administrator read required")
		}
	}
	return command, state, nil
}

// Queue evidence is scoped to the authenticated input and requested event.
func registrationQueueContact(input agent.Input, event string, id int64) bool {
	if input.Registration == nil || id <= 0 {
		return false
	}
	for _, read := range input.Registration.Reads {
		if read.Error != "" || read.Omitted || read.Request.Event != event ||
			read.Request.View != registrationQueueView {
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

func RegistrationEventKnown(context *agent.RegistrationContext, event string) bool {
	if event == "" {
		return true
	}
	for _, item := range context.Events {
		if item.ID == event {
			return true
		}
	}
	for _, read := range context.Reads {
		if read.Error != "" || read.Historical {
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

func RegistrationHistoricalEventKnown(context *agent.RegistrationContext, event string) bool {
	for _, read := range context.Reads {
		if read.Error == "" && !read.Omitted && read.Request.Event == event && read.Historical {
			return true
		}
	}
	return false
}

func registrationOwn(context *agent.RegistrationContext, event string) *passbooking.Booking {
	for _, read := range slices.Backward(context.Reads) {
		if read.Error == "" && !read.Historical && read.Booking != nil && read.Booking.Event == event {
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
		if read.Request.Event == event && read.Request.View == registrationQueueView && read.Error == "" {
			return true
		}
	}
	return false
}

func registrationTargetCommand(context *agent.RegistrationContext, p agent.RegistrationProposal,
	actorVersion int64,
) (passbooking.Command, bool) {
	for _, read := range slices.Backward(context.Reads) {
		if read.Error != "" || read.Request.Event != p.Event {
			continue
		}
		if p.Name == "accept" || p.Name == "decline" {
			for _, invite := range read.Invitations {
				if invite.From.Owner == p.Target {
					return RegistrationInvitationCommand(p.Name, p.Event, actorVersion, invite), true
				}
			}
		} else {
			for _, booking := range read.Queue {
				if booking.Owner == p.Target {
					return RegistrationQueueCommand(p.Name, p.Event, actorVersion, booking), true
				}
			}
		}
	}
	return passbooking.Command{}, false
}

func GroundedRegistrationContact(requestEvidence string, input agent.Input, id int64) bool {
	if id <= 0 || input.Registration == nil {
		return false
	}
	if slices.Contains(input.Registration.TrustedPartnerIDs, id) {
		return true
	}
	needle := strconv.FormatInt(id, 10)
	tokens := strings.FieldsFunc(requestEvidence, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '@'
	})
	return slices.Contains(tokens, needle)
}

func RegistrationAdminTargetGrounded(requestEvidence string, input agent.Input, p agent.RegistrationProposal) bool {
	id, err := strconv.ParseInt(p.Target, 10, 64)
	if err != nil || input.Registration == nil {
		return false
	}
	if GroundedRegistrationContact(requestEvidence, input, id) || input.Registration.AdminTargetTelegramID == id {
		return true
	}
	for _, read := range input.Registration.Reads {
		if read.Error != "" || read.Request.Event != p.Event || read.Request.View != registrationQueueView {
			continue
		}
		for _, booking := range read.Queue {
			if booking.TelegramID == id {
				return true
			}
		}
	}
	return false
}

func BindAdminAssignment(requestEvidence string,
	plan agent.Plan,
	input agent.Input,
) (*passbooking.AdminAssignment, *RegistrationMenu, error) {
	p := plan.RegistrationAction
	if p == nil || p.Assignment == nil || input.Registration == nil {
		return nil, nil, errors.New("assignment context required")
	}
	target := adminAssignmentEvidence(input.Registration, p.Event, p.Target)
	if target == nil || !target.CanAssign {
		return nil, nil, errors.New("assignment target lacks evidence")
	}
	options := p.Assignment
	bound := RegistrationAssignmentDraft(p.Event, *target, nil)
	command := &bound
	command.TotalPrice, command.Kind, command.Comment = options.TotalPrice, options.Kind, options.Comment
	command.SkipBalance, command.AppendTier = options.SkipBalance, options.AppendTier
	if options.Create {
		if options.FromProfile && !target.CanCreateFromProfile {
			return nil, nil, errors.New("profile cannot supply assignment defaults")
		}
		if options.LegalName != nil && !strings.Contains(requestEvidence, *options.LegalName) {
			return nil, nil, errors.New("assignment name lacks current evidence")
		}
		command.Create = &passbooking.AdminCreate{
			ProfileVersion: target.ProfileVersion,
			FromProfile:    options.FromProfile,
			Role:           passallocation.Role(options.Role),
			LegalName:      options.LegalName,
		}
	}
	state := &RegistrationMenu{
		Event:                 p.Event,
		View:                  agent.RegistrationAdminTarget,
		AdminTargetTelegramID: target.Booking.TelegramID,
	}
	return command, state, nil
}

func adminAssignmentEvidence(context *agent.RegistrationContext, event, owner string) *passbooking.AdminTarget {
	for _, read := range slices.Backward(context.Reads) {
		if read.Error == "" && read.Request.Event == event && read.Request.View == agent.RegistrationAdminTarget &&
			read.AdminTarget != nil && read.AdminTarget.Booking.Owner == owner {
			return read.AdminTarget
		}
	}
	return nil
}

func bindAdminTargetView(
	context *agent.RegistrationContext,
	p agent.RegistrationProposal,
	state *RegistrationMenu,
) error {
	if p.View != agent.RegistrationAdminTarget || p.Target == "" {
		return nil
	}
	target := adminAssignmentEvidence(context, p.Event, p.Target)
	if target == nil {
		return errors.New("assignment target lacks evidence")
	}
	state.AdminTargetTelegramID = target.Booking.TelegramID
	return nil
}

func HistoricalPassView(view string) bool {
	return view == "" || view == "home" || view == "payment"
}

func registrationPlanEventKnown(context *agent.RegistrationContext, proposal agent.RegistrationProposal) bool {
	if context == nil {
		return false
	}
	if RegistrationEventKnown(context, proposal.Event) {
		return true
	}
	return proposal.Name == agent.RegistrationShow && HistoricalPassView(proposal.View) &&
		RegistrationHistoricalEventKnown(context, proposal.Event)
}

func registrationReviewEvidence(context *agent.RegistrationContext, p agent.RegistrationProposal) *passbooking.Payment {
	for _, read := range slices.Backward(context.Reads) {
		if read.Request.Event != p.Event || read.Request.View != "payment_queue" || read.Error != "" {
			continue
		}
		for _, item := range read.PaymentQueue {
			if item.Owner == p.Target && item.Payment.Decision == "pending" {
				return &item.Payment
			}
		}
	}
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
) (*passbooking.Command, *RegistrationMenu, error) {
	target := takeoverEvidence(value, p.Event, p.Target)
	if target == nil {
		return nil, nil, errors.New("takeover target lacks evidence")
	}
	state := &RegistrationMenu{
		Event:                 p.Event,
		View:                  agent.RegistrationTakeoverTarget,
		AdminTargetTelegramID: target.Booking.TelegramID,
	}
	if p.Name == agent.RegistrationShow {
		return nil, state, nil
	}
	command := RegistrationTakeoverCommand(p.Name, p.Event, *target)
	return &command, state, nil
}

func TakeoverEventGrounded(requestEvidence string, input agent.Input, event string) bool {
	if RegistrationEventKnown(input.Registration, event) || input.Registration.CurrentEvent == event {
		return true
	}
	tokens := strings.FieldsFunc(requestEvidence, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})
	return event != "" && slices.Contains(tokens, event)
}
