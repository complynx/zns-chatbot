package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func historicalPassView(view string) bool {
	return view == "" || view == passMenuHome || view == registrationPayment
}

func (b *Bot) reauthorizeOwnerPass(ctx context.Context, owner string, read *agent.RegistrationReadResult) error {
	booking, err := b.API.PassBooking(ctx, owner, read.Request.Event)
	if err != nil && passMenuFailure(err) != nil {
		return err
	}
	if err != nil || booking.Version == 0 {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: mediaForbidden}
	} else if read.Booking != nil && !samePassSnapshot(*read.Booking, booking) {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: "stale"}
	}
	return nil
}

// Creation identity prevents a recreated booking with a reset version from
// authorizing an earlier snapshot. Missing legacy identity fails closed.
func samePassSnapshot(previous, current passbooking.Booking) bool {
	return agenthost.PassIdentity(previous).Matches(current)
}

// Historical menus expose only owner reads and existing receipt downloads.
func (r *passMenuRenderer) historicalPass(ctx context.Context) error {
	booking, err := r.bot.API.PassBooking(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	if booking.Version == 0 || !historicalPassView(r.state.View) {
		return &core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden}
	}
	r.lines = append(r.lines, r.bookingText(booking))
	if r.state.View != registrationPayment {
		if booking.State == statePaid || booking.State == registrationAssigned {
			r.navigate(i18n.RegistrationPayment, registrationPayment)
		}
		return nil
	}
	payment, err := r.bot.API.PassPayment(ctx, r.owner, r.state.Event, r.owner)
	if historicalPaymentAbsent(err) {
		r.lines = append(r.lines, r.text(i18n.RegistrationPaymentAbsent))
		return nil
	}
	if err != nil {
		return err
	}
	r.lines = append(r.lines, r.paymentStatus(payment))
	r.paymentFile(r.owner, payment, "")
	return nil
}

func historicalPaymentAbsent(err error) bool {
	problem, ok := errors.AsType[*core.ProblemError](err)
	return ok && problem.Status == http.StatusNotFound && problem.Code == "pass_payment_missing"
}
