package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (r *passMenuRenderer) takeoverLink(id int64, label string) {
	state := interaction.RegistrationMenu{
		Event:                 r.state.Event,
		View:                  agent.RegistrationTakeoverTarget,
		AdminTargetTelegramID: id,
	}
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
	view, err := (interaction.RegistrationMenuReader{Domain: r.bot.API}).
		Takeover(ctx, r.owner, r.state.Event, r.state.AdminTargetTelegramID)
	if err != nil {
		return err
	}
	target := view.Target
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
	if view.Takeover != nil {
		r.command(i18n.RegistrationTakeoverApply, *view.Takeover)
	}
	if view.ReceivedOnly != nil {
		r.command(i18n.RegistrationReceiverApply, *view.ReceivedOnly)
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

func (r *passMenuRenderer) takeoverContact(contact *passbooking.Contact) string {
	if contact == nil {
		return r.text(i18n.RegistrationReceiverMissing)
	}
	r.contact(*contact)
	return passMenuLabel(contact.Name)
}
