package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (b *Bot) applyLegacyPractitioner(
	ctx context.Context,
	in incoming,
	state massageView,
	parts []string,
	update int64,
) (bool, massageView, i18n.ID, error) {
	switch parts[1] {
	case actionCreateOrder, "notifications", massageInstant, "clientlist", "sped":
	default:
		return false, state, "", nil
	}
	if len(parts) > legacyCallbackArgument {
		return true, state, i18n.MassageStale, nil
	}
	argument := ""
	if len(parts) == legacyCallbackArgument {
		argument = parts[2]
	}
	next, err := b.legacyPractitionerState(ctx, in.owner, parts[1], argument, update)
	if err != nil {
		failureState, notice, failure := massageFailure(state, "", err)
		return true, failureState, notice, failure
	}
	return true, next, "", nil
}

func legacyInvalidCallback() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: massageLegacyInvalidCallback}
}

func (b *Bot) legacyPractitionerState(
	ctx context.Context,
	owner, action, argument string,
	update int64,
) (massageView, error) {
	next := massageView{Event: b.currentOrderEvent()}
	key := "update-" + strconv.FormatInt(update, 10)
	var err error
	switch action {
	case actionCreateOrder:
		if argument != "" {
			return next, legacyInvalidCallback()
		}
		next.View = "parties"
	case "notifications":
		next.View = "preferences"
		err = b.legacyNotificationCallback(ctx, owner, next.Event, key, argument)
	case massageInstant:
		next.View = massageClients
		err = b.legacyInstantCallback(ctx, owner, next.Event, key, argument)
	case "clientlist":
		next.View = massageLegacyClients
		next.Party, err = b.legacyClientParty(ctx, owner, next.Event, argument)
	case "sped":
		if argument == "" {
			return next, legacyInvalidCallback()
		}
		var booking massage.Reservation
		err = b.API.call(
			ctx,
			owner,
			http.MethodGet,
			"/v1/massage/legacy-practitioner-booking?"+url.Values{
				knowledgeEventQuery: {next.Event},
				"id":                {argument},
			}.Encode(),
			nil,
			&booking,
		)
		next.View = massageLegacyClients
		next.Party = booking.Party
		next.LegacyBooking = booking.ID
	}
	return next, err
}

func (b *Bot) legacyNotificationCallback(ctx context.Context, owner, event, key, argument string) error {
	if argument == "" || argument == "0" {
		_, err := b.API.MassagePreferences(ctx, owner, event)
		return err
	}
	choice, err := legacyCallbackInteger(argument)
	if err != nil {
		return err
	}
	var result massage.Preferences
	command := massage.LegacyCommand{Event: event, Key: key, Choice: choice}
	return b.API.call(ctx, owner, http.MethodPost, "/v1/massage/legacy-preferences", command, &result)
}

func (b *Bot) legacyInstantCallback(ctx context.Context, owner, event, key, argument string) error {
	if argument == "" {
		argument = "1"
	}
	length, err := legacyCallbackInteger(argument)
	if err != nil {
		return err
	}
	var result massage.Reservation
	command := massage.LegacyCommand{Event: event, Key: key, Length: length}
	return b.API.call(ctx, owner, http.MethodPost, "/v1/massage/legacy-instant", command, &result)
}

func (b *Bot) legacyClientParty(ctx context.Context, owner, event, argument string) (string, error) {
	parties, err := b.API.MassageParties(ctx, owner, event)
	if err != nil {
		return "", err
	}
	if len(parties) == 0 {
		return "", legacyInvalidCallback()
	}
	if argument == "" {
		return parties[0].ID, nil
	}
	day, err := legacyCallbackInteger(argument)
	if err != nil {
		return "", err
	}
	const maxDay = 31
	if day < 1 || day > maxDay {
		return "", legacyInvalidCallback()
	}
	target := "legacy-massage-party:" + event + ":" + strconv.Itoa(day)
	for _, party := range parties {
		if party.ID == target {
			return target, nil
		}
	}
	return "", legacyInvalidCallback()
}
