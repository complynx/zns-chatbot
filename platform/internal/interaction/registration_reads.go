package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const registrationReadBytes = 16 * 1024
const registrationEventsView = "events"
const registrationQueueView = "queue"
const registrationInvitationsView = "invitations"
const registrationPaymentView = "payment"
const registrationPaymentQueueView = "payment_queue"

// RegistrationReadClient performs current owner-authorized domain reads.
type RegistrationReadClient interface {
	PassEvents(context.Context, string) ([]passbooking.Event, error)
	PassBooking(context.Context, string, string) (passbooking.Booking, error)
	PassAdminTarget(context.Context, string, string, int64) (passbooking.AdminTarget, error)
	PassTakeoverTarget(context.Context, string, string, int64) (passbooking.TakeoverTarget, error)
	PassPayment(context.Context, string, string, string) (passbooking.Payment, error)
	PassPaymentQueue(context.Context, string, string, string) (passbooking.PaymentPage, error)
	PassQueue(context.Context, string, string, string) (passbooking.BookingPage, error)
	PassInvitations(context.Context, string, string, string) (passbooking.InvitationPage, error)
	PassPaymentAdmins(context.Context, string, string) ([]passbooking.Contact, error)
}

// RegistrationReadStore retains budgets and authorizes each stored projection reload.
type RegistrationReadStore interface {
	ReserveRegistration(context.Context, string, int64, agent.RegistrationProposal) (int, error)
	CompleteRegistration(context.Context, string, int64, int, agent.RegistrationReadResult) error
	Registration(context.Context, string, int64) ([]agent.RegistrationReadResult, error)
}

// RegistrationReader owns bounded read admission and typed domain projection.
// The host store retains reservations and rechecks current authority on reload.
type RegistrationReader struct {
	Domain RegistrationReadClient
	Store  RegistrationReadStore
	Menu   func(context.Context, string) (RegistrationMenu, error)
}

// Context loads current events and menu hints before the authorized retained reads.
func (c RegistrationReader) Context(
	ctx context.Context,
	owner string,
	id int64,
	partners []int64,
) (*agent.RegistrationContext, error) {
	events, err := c.Domain.PassEvents(ctx, owner)
	if err != nil {
		return nil, err
	}
	page, next, err := RegistrationEvents(events, "")
	if err != nil {
		return nil, err
	}
	state, err := c.Menu(ctx, owner)
	if err != nil {
		return nil, err
	}
	reads, err := c.Store.Registration(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	value := &agent.RegistrationContext{
		AdminTargetTelegramID: state.AdminTargetTelegramID,
		Events:                page, MoreEvents: next != "", EventCursor: next,
		CurrentEvent: state.Event, PendingPartner: state.View == "invite",
		TrustedPartnerIDs: append([]int64{}, partners...), Reads: reads,
		Remaining: agent.MaxRegistrationReads - len(reads),
	}
	if err = BoundRegistrationContext(value); err != nil {
		return nil, err
	}
	return value, nil
}

// Read reserves a grounded request before fetching and exposes only the authorized reload.
func (c RegistrationReader) Read(
	ctx context.Context,
	owner string,
	id int64,
	p agent.RegistrationProposal,
	input *agent.Input,
	requestEvidence string,
) error {
	if input.Registration == nil || input.Registration.Remaining <= 0 {
		return errors.New("registration read budget exhausted")
	}
	if !registrationCursorKnown(input.Registration, p) {
		return errors.New("registration cursor lacks evidence")
	}
	if (p.View == agent.RegistrationAdminTarget || p.View == agent.RegistrationTakeoverTarget) &&
		!RegistrationAdminTargetGrounded(requestEvidence, *input, p) {
		return errors.New("administrator target lacks evidence")
	}
	if p.View == agent.RegistrationTakeoverTarget && !TakeoverEventGrounded(requestEvidence, *input, p.Event) {
		return errors.New("takeover event lacks evidence")
	}
	index, err := c.Store.ReserveRegistration(ctx, owner, id, p)
	if err != nil {
		return err
	}
	result, err := c.fetch(ctx, owner, p)
	if failure := registrationReadFailure(ctx, err); failure != nil {
		return failure
	}
	if err != nil {
		result = agent.RegistrationReadResult{Request: p, Error: "unavailable"}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(encoded) > registrationReadBytes {
		result = agent.RegistrationReadResult{Request: p, Error: "result_budget", Omitted: true}
	}
	err = c.Store.CompleteRegistration(ctx, owner, id, index, result)
	if err != nil {
		return err
	}
	input.Registration.Reads, err = c.Store.Registration(ctx, owner, id)
	if err != nil {
		return err
	}
	input.Registration.Remaining = agent.MaxRegistrationReads - len(input.Registration.Reads)
	return BoundRegistrationContext(input.Registration)
}

func registrationCursorKnown(context *agent.RegistrationContext, p agent.RegistrationProposal) bool {
	if p.Cursor == "" {
		return true
	}
	if p.View == registrationEventsView && p.Cursor == context.EventCursor {
		return true
	}
	for _, read := range context.Reads {
		if read.Error == "" && read.Request.Event == p.Event && read.Request.View == p.View && read.Next == p.Cursor {
			return true
		}
	}
	return false
}

func (c RegistrationReader) fetch(
	ctx context.Context,
	owner string,
	p agent.RegistrationProposal,
) (agent.RegistrationReadResult, error) {
	result := agent.RegistrationReadResult{Request: p}
	if p.View == agent.RegistrationTakeoverTarget {
		return c.fetchTakeoverTarget(ctx, owner, p, result)
	}
	events, err := c.Domain.PassEvents(ctx, owner)
	if err != nil {
		return result, err
	}
	if p.View == registrationEventsView {
		result.Events, result.Next, err = RegistrationEvents(events, p.Cursor)
		return result, err
	}
	found := false
	for _, event := range events {
		if event.ID == p.Event {
			found = true
			break
		}
	}
	booking, err := c.Domain.PassBooking(ctx, owner, p.Event)
	if err != nil {
		return result, err
	}
	if !found {
		if !RegistrationHistoricalAllowed(booking, p.View) {
			result.Error = "unknown_event"
			return result, nil
		}
		result.Historical = true
	}
	result.Booking = &booking
	return c.fetchDetails(ctx, owner, p, result)
}

func (c RegistrationReader) fetchDetails(
	ctx context.Context,
	owner string,
	p agent.RegistrationProposal,
	result agent.RegistrationReadResult,
) (agent.RegistrationReadResult, error) {
	switch p.View {
	case agent.RegistrationTakeoverTarget:
		return c.fetchTakeoverTarget(ctx, owner, p, result)
	case agent.RegistrationAdminTarget:
		telegramID, parseErr := strconv.ParseInt(p.Target, 10, 64)
		if parseErr != nil {
			return result, parseErr
		}
		view, readErr := (RegistrationMenuReader{Domain: c.Domain}).Assignment(ctx, owner, p.Event, telegramID, nil)
		target := view.Target
		target.Name = RegistrationLabel(target.Name)
		result.AdminTarget = &target
		return result, readErr
	case registrationPaymentView:
		if result.Historical {
			payment, readErr := (RegistrationHomeReader{Domain: c.Domain}).HistoricalPayment(ctx, owner, p.Event)
			result.Payment = payment.Payment
			return result, readErr
		}
		payment, readErr := c.Domain.PassPayment(ctx, owner, p.Event, owner)
		result.Payment = &payment
		return result, readErr
	case registrationPaymentQueueView:
		view, readErr := (RegistrationMenuReader{Domain: c.Domain}).
			PaymentQueue(ctx, owner, p.Event, p.Cursor, result.Booking)
		result.PaymentQueue, result.Next = view.Page.Items, view.Page.Next
		return result, readErr
	case registrationQueueView:
		view, readErr := (RegistrationMenuReader{Domain: c.Domain}).Queue(ctx, owner, p.Event, p.Cursor, result.Booking)
		result.Queue, result.Next = view.Page.Bookings, view.Page.Next
		return result, readErr
	case registrationInvitationsView:
		view, readErr := (RegistrationMenuReader{Domain: c.Domain}).Invitations(
			ctx,
			owner,
			p.Event,
			p.Cursor,
			result.Booking,
		)
		result.Invitations, result.Next = view.Page.Invitations, view.Page.Next
		for index := range result.Invitations {
			result.Invitations[index].From.Name = RegistrationLabel(result.Invitations[index].From.Name)
		}
		return result, readErr
	default:
		contacts, readErr := (RegistrationHomeReader{Domain: c.Domain}).Contacts(ctx, owner, p.Event, nil)
		result.PaymentAdmins = contacts.Contacts
		if len(result.PaymentAdmins) > registrationEventLimit {
			result.PaymentAdmins = result.PaymentAdmins[:registrationEventLimit]
			result.Omitted = true
		}
		for index := range result.PaymentAdmins {
			result.PaymentAdmins[index].Name = RegistrationLabel(result.PaymentAdmins[index].Name)
		}
		return result, readErr
	}
}

func (c RegistrationReader) fetchTakeoverTarget(
	ctx context.Context,
	owner string,
	p agent.RegistrationProposal,
	result agent.RegistrationReadResult,
) (agent.RegistrationReadResult, error) {
	id, err := strconv.ParseInt(p.Target, 10, 64)
	if err != nil {
		return result, err
	}
	view, err := (RegistrationMenuReader{Domain: c.Domain}).Takeover(ctx, owner, p.Event, id)
	target := view.Target
	target.Name = RegistrationLabel(target.Name)
	result.TakeoverTarget = &target
	return result, err
}
func registrationReadFailure(ctx context.Context, err error) error {
	if core.IsDatabaseFailure(err) {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	return err
}
