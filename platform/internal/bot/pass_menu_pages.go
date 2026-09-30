package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const passMenuPageSize = 5
const passMenuCursorHistory = 64

func (r *passMenuRenderer) page(total int, next string) (int, int) {
	r.state.Offset = min(max(r.state.Offset, 0), max(total-1, 0)/passMenuPageSize*passMenuPageSize)
	first, last := r.state.Offset, min(r.state.Offset+passMenuPageSize, total)
	if first > 0 || len(r.state.Previous) > 0 {
		state := r.state
		state.Offset -= passMenuPageSize
		if state.Offset < 0 {
			state.After = state.Previous[len(state.Previous)-1]
			state.Previous = state.Previous[:len(state.Previous)-1]
			state.Offset = 20
		}
		r.choices = append(
			r.choices,
			passMenuChoice{label: r.text(i18n.PagePrevious), action: passMenuAction{View: &state}},
		)
	}
	if last < total || next != "" {
		state := r.state
		state.Offset = last
		if last == total {
			state.Previous = append(append([]string{}, state.Previous...), state.After)
			if len(state.Previous) > passMenuCursorHistory {
				state.Previous = state.Previous[1:]
			}
			state.After, state.Offset = next, 0
		}
		r.choices = append(
			r.choices,
			passMenuChoice{label: r.text(i18n.PageNext), action: passMenuAction{View: &state}},
		)
	}
	if first > 0 || r.state.After != "" {
		r.navigate(i18n.RegistrationFirst, r.state.View)
	}
	return first, last
}

func (r *passMenuRenderer) invitations(ctx context.Context) error {
	view, err := (interaction.RegistrationMenuReader{Domain: r.bot.API}).
		Invitations(ctx, r.owner, r.state.Event, r.state.After, nil)
	if err != nil {
		return err
	}
	first, last := r.page(len(view.Entries), view.Page.Next)
	for _, entry := range view.Entries[first:last] {
		invite := entry.Invitation
		r.lines = append(r.lines, passMenuLabel(invite.From.Name))
		r.contact(invite.From)
		for _, command := range []passbooking.Command{entry.Accept, entry.Decline} {
			var id i18n.ID
			switch command.Name {
			case passAccept:
				id = i18n.RegistrationAccept
			case passDecline:
				id = i18n.RegistrationDecline
			}
			r.choices = append(
				r.choices,
				passMenuChoice{
					label:  r.text(id) + " · " + passMenuLabel(invite.From.Name),
					action: passMenuAction{Command: &command},
				},
			)
		}
	}
	return nil
}

func (r *passMenuRenderer) queue(ctx context.Context) error {
	view, err := (interaction.RegistrationMenuReader{Domain: r.bot.API}).
		Queue(ctx, r.owner, r.state.Event, r.state.After, nil)
	if err != nil {
		return err
	}
	first, last := r.page(len(view.Entries), view.Page.Next)
	for _, entry := range view.Entries[first:last] {
		booking := entry.Booking
		r.takeoverLink(booking.TelegramID, " · "+strconv.FormatInt(booking.TelegramID, 10))
		state := interaction.RegistrationMenu{
			Event:                 r.state.Event,
			View:                  agent.RegistrationAdminTarget,
			AdminTargetTelegramID: booking.TelegramID,
		}
		r.choices = append(
			r.choices,
			passMenuChoice{
				label:  r.text(i18n.RegistrationAssignment) + " · " + strconv.FormatInt(booking.TelegramID, 10),
				action: passMenuAction{View: &state},
			},
		)
		label := strconv.FormatInt(booking.TelegramID, 10)
		if name := view.Page.Names[booking.Owner]; name != "" {
			label = passMenuLabel(name) + " · " + label
		}
		r.lines = append(r.lines, label+"\n"+r.bookingText(booking))
		r.contact(passbooking.Contact{Name: label, TelegramID: booking.TelegramID})
		if entry.Cancel == nil {
			continue
		}
		r.choices = append(
			r.choices,
			passMenuChoice{
				label:  r.text(i18n.RegistrationCancel) + " · " + label,
				action: passMenuAction{Command: entry.Cancel},
			},
		)
		if entry.Uncouple != nil {
			r.choices = append(
				r.choices,
				passMenuChoice{
					label:  r.text(i18n.RegistrationUncouple) + " · " + label,
					action: passMenuAction{Command: entry.Uncouple},
				},
			)
		}
	}
	return nil
}

func (r *passMenuRenderer) admins(ctx context.Context) error {
	contacts, err := r.bot.API.PassPaymentAdmins(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	booking, err := r.bot.API.PassBooking(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	first, last := r.page(len(contacts), "")
	for _, contact := range contacts[first:last] {
		r.contact(contact)
		choice := passMenuChoice{label: passMenuLabel(contact.Name)}
		if booking.Version == 0 || booking.State == passStateCancelled {
			state := r.state
			state.View, state.PaymentAdmin = passMenuHome, contact.Owner
			choice.action.View = &state
		} else {
			choice.action.Command = &passbooking.Command{
				Name:         "payment_admin",
				Event:        r.state.Event,
				Version:      booking.Version,
				PaymentAdmin: contact.Owner,
			}
		}
		r.choices = append(r.choices, choice)
	}
	return nil
}

func (r *passMenuRenderer) contact(contact passbooking.Contact) {
	r.choices = append(
		r.choices,
		passMenuChoice{
			label: passMenuLabel(contact.Name),
			url:   "tg://user?id=" + strconv.FormatInt(contact.TelegramID, 10),
		},
	)
}
