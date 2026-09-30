package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func historicalPassView(view string) bool {
	return view == "" || view == passMenuHome || view == registrationPayment
}

// Creation identity prevents a recreated booking with a reset version from
// authorizing an earlier snapshot. Missing legacy identity fails closed.
func samePassSnapshot(previous, current passbooking.Booking) bool {
	return agenthost.PassIdentity(previous).Matches(current)
}

// Historical menus expose only owner reads and existing receipt downloads.
func (r *passMenuRenderer) historicalPass(ctx context.Context) error {
	view, err := (interaction.RegistrationHomeReader{Domain: r.bot.API}).
		Historical(ctx, r.owner, r.state.Event, r.state.View)
	if err != nil {
		if core.IsDatabaseFailure(err) {
			return core.ErrDatabase
		}
		return err
	}
	r.lines = append(r.lines, r.bookingText(view.Booking))
	if r.state.View != registrationPayment {
		if view.ShowPayment {
			r.navigate(i18n.RegistrationPayment, registrationPayment)
		}
		return nil
	}
	if view.PaymentAbsent {
		r.lines = append(r.lines, r.text(i18n.RegistrationPaymentAbsent))
		return nil
	}
	payment := *view.Payment
	r.lines = append(r.lines, r.paymentStatus(payment))
	r.paymentFile(r.owner, payment, "")
	return nil
}
