package interaction

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// RegistrationMenuReader assembles currently authorized registration projections
// and their observed actions. Domain calls retain ACL and transaction ownership;
// Telegram selects a page and renders these typed outcomes.
type RegistrationMenuReader struct {
	Domain RegistrationReadClient
}

type RegistrationInvitationEntry struct {
	Invitation passbooking.Invitation
	Accept     passbooking.Command
	Decline    passbooking.Command
}

type RegistrationInvitationMenu struct {
	Booking passbooking.Booking
	Page    passbooking.InvitationPage
	Entries []RegistrationInvitationEntry
}

type RegistrationQueueEntry struct {
	Booking  passbooking.Booking
	Cancel   *passbooking.Command
	Uncouple *passbooking.Command
}

type RegistrationQueueMenu struct {
	Booking passbooking.Booking
	Page    passbooking.BookingPage
	Entries []RegistrationQueueEntry
}

type RegistrationPaymentEntry struct {
	Review passbooking.PaymentReview
	Accept passbooking.Command
	Reject passbooking.Command
}

type RegistrationPaymentMenu struct {
	Booking passbooking.Booking
	Page    passbooking.PaymentPage
	Entries []RegistrationPaymentEntry
}

type RegistrationAssignmentMenu struct {
	Target  passbooking.AdminTarget
	Draft   *passbooking.AdminAssignment
	Command *passbooking.AdminAssignment
}

type RegistrationTakeoverMenu struct {
	Target       passbooking.TakeoverTarget
	Takeover     *passbooking.Command
	ReceivedOnly *passbooking.Command
}

// An observed booking is the caller's current authorized read, never a grant.
// Agent admission already reads it before page selection. Manual menus pass nil
// to retain their page-before-booking read sequence. The page call always checks
// current domain permissions and never relies on this supplied projection.
func (c RegistrationMenuReader) booking(ctx context.Context, owner, event string,
	observed *passbooking.Booking,
) (passbooking.Booking, error) {
	if observed != nil {
		return *observed, nil
	}
	return c.Domain.PassBooking(ctx, owner, event)
}

func (c RegistrationMenuReader) Invitations(ctx context.Context, owner, event, cursor string,
	observed *passbooking.Booking,
) (RegistrationInvitationMenu, error) {
	page, err := c.Domain.PassInvitations(ctx, owner, event, cursor)
	if err != nil {
		return RegistrationInvitationMenu{}, err
	}
	booking, err := c.booking(ctx, owner, event, observed)
	if err != nil {
		return RegistrationInvitationMenu{}, err
	}
	value := RegistrationInvitationMenu{Booking: booking, Page: page}
	for _, invitation := range page.Invitations {
		value.Entries = append(value.Entries, RegistrationInvitationEntry{Invitation: invitation,
			Accept:  RegistrationInvitationCommand("accept", event, booking.Version, invitation),
			Decline: RegistrationInvitationCommand("decline", event, booking.Version, invitation)})
	}
	return value, nil
}

func (c RegistrationMenuReader) Queue(ctx context.Context, owner, event, cursor string,
	observed *passbooking.Booking,
) (RegistrationQueueMenu, error) {
	page, err := c.Domain.PassQueue(ctx, owner, event, cursor)
	if err != nil {
		return RegistrationQueueMenu{}, err
	}
	booking, err := c.booking(ctx, owner, event, observed)
	if err != nil {
		return RegistrationQueueMenu{}, err
	}
	value := RegistrationQueueMenu{Booking: booking, Page: page}
	for _, target := range page.Bookings {
		entry := RegistrationQueueEntry{Booking: target}
		if target.State != "cancelled" {
			cancel := RegistrationQueueCommand("admin_cancel", event, booking.Version, target)
			entry.Cancel = &cancel
			if target.Partner != "" {
				uncouple := RegistrationQueueCommand("admin_uncouple", event, booking.Version, target)
				entry.Uncouple = &uncouple
			}
		}
		value.Entries = append(value.Entries, entry)
	}
	return value, nil
}

func (c RegistrationMenuReader) PaymentQueue(ctx context.Context, owner, event, cursor string,
	observed *passbooking.Booking,
) (RegistrationPaymentMenu, error) {
	page, err := c.Domain.PassPaymentQueue(ctx, owner, event, cursor)
	if err != nil {
		return RegistrationPaymentMenu{}, err
	}
	booking, err := c.booking(ctx, owner, event, observed)
	if err != nil {
		return RegistrationPaymentMenu{}, err
	}
	value := RegistrationPaymentMenu{Booking: booking, Page: page}
	for _, review := range page.Items {
		value.Entries = append(value.Entries, RegistrationPaymentEntry{
			Review: review,
			Accept: RegistrationPaymentReviewCommand(
				"proof_accept",
				event,
				booking.Version,
				review.Owner,
				review.Payment,
			),
			Reject: RegistrationPaymentReviewCommand(
				"proof_reject",
				event,
				booking.Version,
				review.Owner,
				review.Payment,
			),
		})
	}
	return value, nil
}

func (c RegistrationMenuReader) Assignment(ctx context.Context, owner, event string, targetID int64,
	previous *passbooking.AdminAssignment,
) (RegistrationAssignmentMenu, error) {
	target, err := c.Domain.PassAdminTarget(ctx, owner, event, targetID)
	if err != nil {
		return RegistrationAssignmentMenu{}, err
	}
	value := RegistrationAssignmentMenu{Target: target}
	if !target.CanAssign {
		return value, nil
	}
	command := RegistrationAssignmentDraft(event, target, previous)
	value.Draft = &command
	if target.Booking.Version == 0 || target.Booking.State == "cancelled" {
		if !target.CanCreateFromProfile {
			return value, nil
		}
		command.Create = &passbooking.AdminCreate{FromProfile: true, ProfileVersion: target.ProfileVersion}
	}
	value.Command = &command
	return value, nil
}

func (c RegistrationMenuReader) Takeover(ctx context.Context, owner, event string,
	targetID int64,
) (RegistrationTakeoverMenu, error) {
	target, err := c.Domain.PassTakeoverTarget(ctx, owner, event, targetID)
	if err != nil {
		return RegistrationTakeoverMenu{}, err
	}
	value := RegistrationTakeoverMenu{Target: target}
	if target.Booking.PaymentAdmin != owner || target.Booking.Partner != "" {
		command := RegistrationTakeoverCommand(passbooking.CommandTakeover, target.Booking.Event, target)
		value.Takeover = &command
	}
	if target.CanBackfill {
		command := RegistrationTakeoverCommand(passbooking.CommandReceivedOnly, target.Booking.Event, target)
		value.ReceivedOnly = &command
	}
	return value, nil
}
