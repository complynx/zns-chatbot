package interaction

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// RegistrationHomeReader owns the historical owner view and payment-contact
// selection. Its projections contain only data returned by current domain reads.
type RegistrationHomeReader struct {
	Domain RegistrationReadClient
}

type RegistrationContactSelection struct {
	BookingAdmin string
	PendingAdmin string
}

type RegistrationPaymentContacts struct {
	Contacts []passbooking.Contact
	Selected *passbooking.Contact
}

type RegistrationHistoricalPayment struct {
	Payment *passbooking.Payment
	Absent  bool
}

type RegistrationHistoricalMenu struct {
	Booking       passbooking.Booking
	Payment       *passbooking.Payment
	PaymentAbsent bool
	ShowPayment   bool
}

// Contacts retains the complete domain-authorized projection for agent reads.
// A manual selection uses the booked contact before a pending menu choice and
// skips the read when neither exists. It cannot create a contact absent from the
// authorized projection. Callers retain their own presentation and read budgets.
func (c RegistrationHomeReader) Contacts(ctx context.Context, owner, event string,
	selection *RegistrationContactSelection,
) (RegistrationPaymentContacts, error) {
	selected := ""
	if selection != nil {
		selected = selection.BookingAdmin
		if selected == "" {
			selected = selection.PendingAdmin
		}
		if selected == "" {
			return RegistrationPaymentContacts{}, nil
		}
	}
	contacts, err := c.Domain.PassPaymentAdmins(ctx, owner, event)
	if err != nil {
		return RegistrationPaymentContacts{}, err
	}
	value := RegistrationPaymentContacts{Contacts: contacts}
	for _, contact := range contacts {
		if selected != "" && contact.Owner == selected {
			value.Selected = &contact
			break
		}
	}
	return value, nil
}

// RegistrationHistoricalAllowed limits archived menus to an existing owner's
// booking and the home/payment views. It does not change active-event admission.
func RegistrationHistoricalAllowed(booking passbooking.Booking, view string) bool {
	return booking.Version != 0 && HistoricalPassView(view)
}

// HistoricalPayment treats only the domain's exact missing-payment response as
// absence. Permission, provider and SQL failures remain failures; the owner is
// always the same authenticated principal used for the payment read.
func (c RegistrationHomeReader) HistoricalPayment(ctx context.Context, owner, event string,
) (RegistrationHistoricalPayment, error) {
	payment, err := c.Domain.PassPayment(ctx, owner, event, owner)
	if !core.IsDatabaseFailure(err) && ctx.Err() != nil {
		return RegistrationHistoricalPayment{}, ctx.Err()
	}
	if err != nil {
		if !core.IsDatabaseFailure(err) && ctx.Err() == nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			problem, ok := errors.AsType[*core.ProblemError](err)
			if ok && problem.Status == http.StatusNotFound && problem.Code == "pass_payment_missing" {
				return RegistrationHistoricalPayment{Absent: true}, nil
			}
		}
		return RegistrationHistoricalPayment{}, err
	}
	return RegistrationHistoricalPayment{Payment: &payment}, nil
}

// Historical performs owner-only reads and offers no mutation or privileged
// queue action. Existing receipt download references remain domain-authorized.
func (c RegistrationHomeReader) Historical(ctx context.Context, owner, event, view string,
) (RegistrationHistoricalMenu, error) {
	booking, err := c.Domain.PassBooking(ctx, owner, event)
	if err != nil {
		return RegistrationHistoricalMenu{}, err
	}
	if !RegistrationHistoricalAllowed(booking, view) {
		return RegistrationHistoricalMenu{}, &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	value := RegistrationHistoricalMenu{Booking: booking}
	if view != registrationPaymentView {
		value.ShowPayment = booking.State == "paid" || booking.State == "assigned"
		return value, nil
	}
	payment, err := c.HistoricalPayment(ctx, owner, event)
	if err != nil {
		return RegistrationHistoricalMenu{}, err
	}
	value.Payment, value.PaymentAbsent = payment.Payment, payment.Absent
	return value, nil
}
