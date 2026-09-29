package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

const legacyCallbackBase = 2
const legacyCallbackArgument = 3
const legacyCallbackChoice = 4
const massageLegacyClients = "legacy_clients"
const massageLegacySource = "source"
const massageLegacyBack = "back"
const massageLegacyExitAction = "exit"
const massageLegacyInvalidCallback = "invalid_callback"

func (c APIClient) LegacyMassageDraft(ctx context.Context, owner, event, id string) (massage.LegacyDraft, error) {
	var result massage.LegacyDraft
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/massage/legacy-draft?"+url.Values{knowledgeEventQuery: {event}, "id": {id}}.Encode(),
		nil,
		&result,
	)
	return result, err
}

func (c APIClient) ExecuteLegacyMassage(
	ctx context.Context,
	owner string,
	command massage.LegacyCommand,
) (massage.LegacyDraft, error) {
	var result massage.LegacyDraft
	err := c.call(ctx, owner, http.MethodPost, "/v1/massage/legacy-draft", command, &result)
	return result, err
}

func (b *Bot) applyLegacyMassageCallback(
	ctx context.Context,
	in incoming,
	state massageView,
	update int64,
) (massageView, i18n.ID, error) {
	parts := strings.Split(in.text, "|")
	if len(parts) >= legacyCallbackBase {
		if handled, next, notice, err := b.applyLegacyPractitioner(ctx, in, state, parts, update); handled {
			return next, notice, err
		}
	}
	if len(parts) == legacyCallbackBase && (parts[1] == "start" || parts[1] == massageLegacyExitAction) {
		state.View = massageHome
		if parts[1] == massageLegacyExitAction {
			state.View = massageLegacyExit
		}
		return state, "", nil
	}
	if len(parts) < legacyCallbackArgument || len(parts) > legacyCallbackChoice {
		return state, i18n.MassageStale, nil
	}
	command := massage.LegacyCommand{
		ID:    parts[2],
		Event: b.currentOrderEvent(),
		Key:   "source-" + strconv.FormatInt(update, 10),
	}
	switch parts[1] {
	case "ed":
		command.Action = massageLegacySource
		if len(parts) == legacyCallbackChoice {
			choice, err := legacyCallbackInteger(parts[3])
			if err != nil {
				return massageFailure(state, "", err)
			}
			command.Choice = choice
		}
	case massageLegacyBack:
		command.Action = "source_back"
	case "xed":
		command.Action = "source_cancel"
	default:
		return state, i18n.MassageStale, nil
	}
	return b.applyLegacyMassage(ctx, in.owner, state, command)
}

func (b *Bot) applyLegacyMassage(
	ctx context.Context,
	owner string,
	state massageView,
	command massage.LegacyCommand,
) (massageView, i18n.ID, error) {
	draft, err := b.API.ExecuteLegacyMassage(ctx, owner, command)
	if err != nil {
		return massageFailure(state, "", err)
	}
	state = massageView{Event: draft.Event, View: "legacy", LegacyID: draft.ID}
	if draft.Closed {
		return closedLegacyMassage(state, draft, command)
	}
	return state, "", nil
}

func closedLegacyMassage(
	state massageView,
	draft massage.LegacyDraft,
	c massage.LegacyCommand,
) (massageView, i18n.ID, error) {
	state.View = massageMine
	if draft.Cancelled {
		return state, i18n.MassageCancelled, nil
	}
	back := c.Action == "source_back" || c.Action == massageLegacyBack ||
		(c.Action == massageLegacySource && c.Choice < 0)
	if draft.Booking != "" {
		if back || (c.Action == massageLegacySource && c.Choice == 0) {
			return state, "", nil
		}
		return state, i18n.MassageSaved, nil
	}
	if back {
		state.View = massageHome
		return state, "", nil
	}
	state.View = massageLegacyExit
	return state, i18n.MassageCancelled, nil
}

func (r *massageRenderer) legacyChoice(
	label string,
	d massage.LegacyDraft,
	action string,
	length int,
	choice massage.LegacyChoice,
) {
	c := massage.LegacyCommand{
		ID:        d.ID,
		Event:     d.Event,
		Version:   d.Version,
		Action:    action,
		Length:    length,
		Selection: choice,
	}
	r.choices = append(r.choices, massageChoice{label: label, action: massageButtonAction{Legacy: &c}})
}

func (r *massageRenderer) legacy(ctx context.Context) error {
	d, err := r.bot.API.LegacyMassageDraft(ctx, r.owner, r.state.Event, r.state.LegacyID)
	if err != nil {
		return err
	}
	if d.Closed {
		r.state.View = massageMine
		return r.bookings(ctx)
	}
	if d.State.Length == 0 {
		r.lines = append(r.lines, r.text(i18n.MassageLength))
		for _, length := range []int{1, 2, 3, 5} {
			r.legacyChoice(r.quote(length), d, "length", length, massage.LegacyChoice{})
		}
	} else {
		if err = r.legacySlots(ctx, d); err != nil {
			return err
		}
		r.legacyChoice(r.text(i18n.MassageLength), d, massageLegacyBack, 0, massage.LegacyChoice{})
	}
	r.legacyChoice(r.text(i18n.MassageCancel), d, "cancel", 0, massage.LegacyChoice{})
	return nil
}

func (r *massageRenderer) legacySlots(ctx context.Context, d massage.LegacyDraft) error {
	parties, err := r.bot.API.MassageParties(ctx, r.owner, d.Event)
	if err != nil {
		return err
	}
	r.lines = append(r.lines, r.text(i18n.MassageSlots)+"\n"+r.quote(d.State.Length))
	selectedAvailable := false
	for _, party := range parties {
		if !party.Open && party.End.After(time.Now()) {
			r.legacyChoice(massageWhen(party.Start), d, "select", 0, massage.LegacyChoice{Party: party.ID})
			selectedAvailable = selectedAvailable || party.ID == d.State.Party
		}
	}
	if !selectedAvailable {
		if d.State.Party != "" {
			r.lines = append(r.lines, r.text(i18n.MassageStale))
		}
		return nil
	}
	available, err := r.bot.API.MassageSlots(ctx, r.owner, d.Event, d.State.Party, d.State.Length)
	if err != nil {
		return err
	}
	r.legacySlotChoices(d, available)
	return nil
}

func (r *massageRenderer) legacySlotChoices(d massage.LegacyDraft, available massage.Availability) {
	names := map[string]string{}
	for _, provider := range available.Providers {
		if d.State.Length < provider.MinLength || d.State.Length > provider.MaxLength {
			continue
		}
		names[provider.Owner] = massageName(provider.Name)
		mark := "✅ "
		if selected, present := d.State.Selected[provider.Owner]; present && !selected {
			mark = "❌ "
		}
		r.legacyChoice(mark+names[provider.Owner], d, "select", 0, massage.LegacyChoice{Specialist: provider.Owner})
	}
	slots := []massage.SlotChoice{}
	for _, slot := range available.Slots {
		if selected, present := d.State.Selected[slot.Specialist]; !present || selected {
			slots = append(slots, slot)
		}
	}
	const pageSize = 24
	page := max(0, min(d.State.Page, (max(1, len(slots))-1)/pageSize))
	first, last := page*pageSize, min((page+1)*pageSize, len(slots))
	for _, slot := range slots[first:last] {
		slotID := slot.Slot
		r.legacyChoice(
			names[slot.Specialist]+" "+massageWhen(slot.Start),
			d,
			"select",
			0,
			massage.LegacyChoice{Slot: &slotID, Specialist: slot.Specialist},
		)
	}
	if page > 0 {
		r.legacyPage("←", d, page-1)
	}
	if last < len(slots) {
		r.legacyPage("→", d, page+1)
	}
}

func legacyCallbackInteger(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &core.ProblemError{Status: http.StatusBadRequest, Code: massageLegacyInvalidCallback}
	}
	return value, nil
}

func (r *massageRenderer) legacyPage(label string, d massage.LegacyDraft, page int) {
	c := massage.LegacyCommand{ID: d.ID, Event: d.Event, Version: d.Version, Action: "page", Choice: page}
	r.choices = append(r.choices, massageChoice{label: label, action: massageButtonAction{Legacy: &c}})
}
