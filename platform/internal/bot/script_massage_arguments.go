package bot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func massageIdentifier(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 200 }

func (b *Bot) bindMassageTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
) (scriptMassageRequest, error) {
	switch call.Name {
	case scriptMassageBook:
		return b.bindMassageBooking(ctx, owner, call)
	case scriptMassageCancel:
		return b.bindMassageCancellation(ctx, owner, call)
	case scriptMassageInstant:
		var args struct {
			Event  string `json:"event"`
			Party  string `json:"party"`
			Length int    `json:"length"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return scriptMassageRequest{}, err
		}
		if !massageIdentifier(args.Event) || !massageIdentifier(args.Party) || args.Length < 1 || args.Length > 6 {
			return scriptMassageRequest{}, errors.New("invalid tool arguments")
		}
		if _, err := b.API.MassagePreferences(ctx, owner, args.Event); err != nil {
			return scriptMassageRequest{}, err
		}
		return scriptMassageRequest{
			Event:   args.Event,
			Command: &massage.Command{Action: "instant", Event: args.Event, Party: args.Party, Length: args.Length},
		}, nil
	case scriptMassageConfigure:
		var args struct {
			Event    string `json:"event"`
			Bookings *bool  `json:"bookings"`
			Next     *bool  `json:"next"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return scriptMassageRequest{}, err
		}
		if !massageIdentifier(args.Event) || args.Bookings == nil || args.Next == nil {
			return scriptMassageRequest{}, errors.New("invalid tool arguments")
		}
		if _, err := b.API.MassagePreferences(ctx, owner, args.Event); err != nil {
			return scriptMassageRequest{}, err
		}
		return scriptMassageRequest{
			Event:       args.Event,
			Preferences: &massage.Preferences{Bookings: *args.Bookings, Next: *args.Next},
		}, nil
	default:
		return scriptMassageRequest{}, errors.New("tool unavailable")
	}
}

func (b *Bot) bindMassageBooking(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
) (scriptMassageRequest, error) {
	var args struct {
		Event      string    `json:"event"`
		Party      string    `json:"party"`
		Specialist string    `json:"specialist"`
		Start      time.Time `json:"start"`
		Length     int       `json:"length"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return scriptMassageRequest{}, err
	}
	if !massageIdentifier(args.Event) || !massageIdentifier(args.Party) || !massageIdentifier(args.Specialist) ||
		args.Start.IsZero() {
		return scriptMassageRequest{}, errors.New("invalid tool arguments")
	}
	available, err := b.API.MassageSlots(ctx, owner, args.Event, args.Party, args.Length)
	if err != nil {
		return scriptMassageRequest{}, err
	}
	for _, slot := range available.Slots {
		if slot.Specialist == args.Specialist && slot.Start.Equal(args.Start) {
			return scriptMassageRequest{Event: args.Event, Command: &massage.Command{
				Action:        "book",
				Event:         args.Event,
				Party:         args.Party,
				Specialist:    args.Specialist,
				Slot:          slot.Slot,
				Length:        args.Length,
				ExpectedStart: &args.Start,
			}}, nil
		}
	}
	return scriptMassageRequest{}, errors.New("unknown resource")
}

func (b *Bot) bindMassageCancellation(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
) (scriptMassageRequest, error) {
	var args struct {
		Event   string `json:"event"`
		Booking string `json:"booking"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return scriptMassageRequest{}, err
	}
	if !massageIdentifier(args.Event) || !massageIdentifier(args.Booking) {
		return scriptMassageRequest{}, errors.New("invalid tool arguments")
	}
	bookings, err := b.API.MassageBookings(ctx, owner, args.Event, "", "mine")
	if err != nil {
		return scriptMassageRequest{}, err
	}
	for _, booking := range bookings {
		if booking.ID == args.Booking && booking.Owner == owner {
			return scriptMassageRequest{
				Event: args.Event,
				Command: &massage.Command{Action: "cancel", Event: args.Event,
					Booking: booking.ID, Version: booking.Version},
			}, nil
		}
	}
	return scriptMassageRequest{}, errors.New("unknown resource")
}
