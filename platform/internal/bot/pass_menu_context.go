package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (r *passMenuRenderer) eventHeading(ctx context.Context) error {
	if r.state.Event == "" || r.state.View == passMenuEvents {
		return nil
	}
	events, err := r.bot.API.PassEvents(ctx, r.owner)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.ID == r.state.Event {
			label := event.Title(r.language, false)
			r.lines = append([]string{passMenuLabel(label)}, r.lines...)
			return nil
		}
	}
	if r.state.Historical {
		r.lines = append([]string{passMenuLabel(r.state.Event)}, r.lines...)
	}
	return nil
}

func (r *passMenuRenderer) paymentContact(ctx context.Context, booking passbooking.Booking) error {
	owner := booking.PaymentAdmin
	if owner == "" {
		owner = r.state.PaymentAdmin
	}
	if owner == "" {
		return nil
	}
	contacts, err := r.bot.API.PassPaymentAdmins(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	for _, contact := range contacts {
		if contact.Owner == owner {
			r.lines = append(r.lines, r.text(i18n.RegistrationAdmin)+": "+passMenuLabel(contact.Name))
			r.contact(contact)
			break
		}
	}
	return nil
}
