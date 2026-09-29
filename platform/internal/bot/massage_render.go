package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type massageChoice struct {
	label  string
	url    string
	webApp *telegram.WebApp
	action massageButtonAction
}
type massageRenderer struct {
	bot             *Bot
	owner, language string
	state           massageView
	lines           []string
	choices         []massageChoice
	clientLinks     map[string]string
}

func (r *massageRenderer) text(id i18n.ID) string {
	value, _ := i18n.Translate(r.language, id, nil)
	return value
}
func (r *massageRenderer) view(label string, state massageView) {
	r.choices = append(r.choices, massageChoice{label: label, action: massageButtonAction{View: &state}})
}
func (r *massageRenderer) navigation(id i18n.ID, view string) {
	state := r.state
	state.View, state.Page = view, 0
	r.view(r.text(id), state)
}
func massageWhen(instant time.Time) string {
	const offset = 3 * 60 * 60
	return instant.In(time.FixedZone("Minsk", offset)).Format("02.01 15:04")
}
func massageName(text string) string {
	const limit = 60
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}
func (r *massageRenderer) quote(length int) string {
	duration, price, _ := massage.Quote(length, massage.BYN)
	value, _ := i18n.Translate(
		r.language,
		i18n.MassageQuote,
		map[string]string{"minutes": strconv.Itoa(int(duration.Minutes())), "price": strconv.Itoa(price)},
	)
	return value
}

func (b *Bot) RenderMassage(ctx context.Context, owner string, chat int64, notice i18n.ID) error {
	state, revision, err := b.massageState(ctx, owner)
	if err != nil {
		return err
	}
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	r := massageRenderer{bot: b, owner: owner, language: preference.Language, state: state}
	err = r.build(ctx)
	if err != nil {
		_, _, failure := massageFailure(state, "", err)
		if failure != nil {
			return failure
		}
		r.state.View, r.state.Page = massageHome, 0
		r.lines, r.choices = nil, nil
		if err = r.home(ctx); err != nil {
			return err
		}
		notice = i18n.MassageStale
	}
	if r.state.View != massageHome && r.state.View != massageLegacyExit {
		r.navigation(i18n.MassageHome, massageHome)
	}
	if len(r.lines) == 0 {
		r.lines = append(r.lines, r.text(i18n.MassageEmpty))
	}
	text := r.text(i18n.MassageTitle) + "\n" + r.text(i18n.MassageTimeZone) + "\n\n" + strings.Join(r.lines, "\n\n")
	if notice != "" {
		text = r.text(notice) + "\n\n" + text
	}
	if err = b.saveMassageState(ctx, owner, chat, revision, r.state); err != nil {
		return err
	}
	rows, tokens, err := b.massageButtons(ctx, owner, revision, r.choices)
	if err != nil {
		return err
	}
	ctx = withBotCard(
		ctx,
		botdelivery.Reference{
			Family:       botFamilyMassage,
			CardKey:      botFamilyMassage,
			Revision:     revision,
			Notice:       notice,
			Continuation: botdelivery.Continuation{Tokens: tokens},
		},
	)
	if err = b.deliverMassageCard(
		ctx,
		owner,
		telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: rows}},
	); err != nil {
		return err
	}
	return nil
}

func (b *Bot) deliverMassageCard(ctx context.Context, owner string, payload telegram.Send) error {
	hash, err := botCardHash(payload)
	if err != nil {
		return err
	}
	var previous string
	if err = b.DB.QueryRow(ctx, "SELECT message_id,view_hash FROM bot.massage_views WHERE owner=$1", owner).
		Scan(&payload.MessageID, &previous); err != nil {
		return err
	}
	if payload.MessageID > 0 && previous == hash {
		return nil
	}
	_, revision, err := b.massageState(ctx, owner)
	if err != nil {
		return err
	}
	ref := botdelivery.Reference{Family: botFamilyMassage, CardKey: botFamilyMassage, Revision: revision}
	if selected, ok := ctx.Value(botCardContextKey{}).(botdelivery.Reference); ok {
		ref = selected
	}
	return b.queueBotCard(
		ctx,
		owner,
		payload,
		ref,
		botdelivery.Continuation{
			Kind:     "massage_card",
			Revision: revision,
			ViewHash: hash,
			Tokens:   ref.Continuation.Tokens,
		},
	)
}
func (b *Bot) massageButtons(
	ctx context.Context,
	owner string,
	revision int64,
	choices []massageChoice,
) ([][]telegram.Button, []string, error) {
	rows := make([][]telegram.Button, 0, len(choices))
	tokens := make([]string, 0, len(choices))
	for _, choice := range choices {
		if choice.webApp != nil {
			rows = append(rows, []telegram.Button{{Text: choice.label, WebApp: choice.webApp}})
			continue
		}
		if choice.url != "" {
			rows = append(rows, []telegram.Button{{Text: choice.label, URL: choice.url}})
			continue
		}
		encoded, err := json.Marshal(choice.action)
		if err != nil {
			return nil, nil, err
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("massage/%s/%d/%s", owner, revision, encoded)))
		token := hex.EncodeToString(sum[:16])
		_, err = b.DB.Exec(ctx, `INSERT INTO bot.massage_buttons(owner,token,revision,action) VALUES($1,$2,$3,$4)
		ON CONFLICT(owner,token) DO NOTHING`, owner, token, revision, choice.action)
		if err != nil {
			return nil, nil, err
		}
		tokens = append(tokens, token)
		rows = append(rows, []telegram.Button{{Text: choice.label, Data: massagePrefix + token}})
	}
	return rows, tokens, nil
}

func (r *massageRenderer) build(ctx context.Context) error {
	switch r.state.View {
	case massageLegacyExit:
		r.lines = append(r.lines, r.text(i18n.MassageExited))
		return nil
	case "legacy":
		return r.legacy(ctx)
	case "parties":
		return r.parties(ctx)
	case massageLength:
		return r.lengths(false)
	case massageProviders, "slots":
		return r.selection(ctx)
	case massageMine, massageClients, "timetable", massageLegacyClients:
		return r.bookings(ctx)
	case "preferences":
		return r.preferences(ctx)
	case massageInstant:
		return r.instant(ctx)
	default:
		return r.home(ctx)
	}
}

func (r *massageRenderer) home(ctx context.Context) error {
	r.lines = append(r.lines, r.text(i18n.MassageTitle))
	r.navigation(i18n.MassageBook, "parties")
	r.navigation(i18n.MassageMine, massageMine)
	_, err := r.bot.API.MassagePreferences(ctx, r.owner, r.state.Event)
	if err == nil {
		r.navigation(i18n.MassageClients, massageClients)
		r.navigation(i18n.MassagePreferences, "preferences")
		r.navigation(i18n.MassageInstant, massageInstant)
		r.navigation(i18n.MassageTimetable, "timetable")
		r.webTimetable()
		return nil
	}
	if _, _, failure := massageFailure(r.state, "", err); failure != nil {
		return failure
	}
	_, err = r.bot.API.MassageBookings(ctx, r.owner, r.state.Event, "", "timetable")
	if err == nil {
		r.navigation(i18n.MassageTimetable, "timetable")
		r.webTimetable()
		return nil
	}
	_, _, err = massageFailure(r.state, "", err)
	return err
}

func (r *massageRenderer) page(total, size int) (int, int) {
	r.state.Page = min(max(r.state.Page, 0), max(total-1, 0)/size)
	first := r.state.Page * size
	last := min(first+size, total)
	if first > 0 {
		state := r.state
		state.Page--
		r.view(r.text(i18n.PagePrevious), state)
	}
	if last < total {
		state := r.state
		state.Page++
		r.view(r.text(i18n.PageNext), state)
	}
	return first, last
}

func (r *massageRenderer) parties(ctx context.Context) error {
	parties, err := r.bot.API.MassageParties(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	filtered := []massage.EventParty{}
	for _, party := range parties {
		if !party.Open && time.Now().Before(party.End.Add(massagePartyTolerance)) {
			filtered = append(filtered, party)
		}
	}
	r.lines = append(r.lines, r.text(i18n.MassageParties))
	first, last := r.page(len(filtered), massageChoicePageSize)
	for _, party := range filtered[first:last] {
		state := r.state
		state.View, state.Party, state.Page = massageLength, party.ID, 0
		r.view(massageWhen(party.Start), state)
	}
	return nil
}

func (r *massageRenderer) lengths(instant bool) error {
	r.lines = append(r.lines, r.text(i18n.MassageLength))
	lengths := []int{1, 2, 3, 5}
	if instant {
		lengths = []int{1, 2, 3, 4, 5, 6}
	}
	for _, length := range lengths {
		if instant {
			command := massage.Command{
				Action: massageInstant,
				Event:  r.state.Event,
				Party:  r.state.Party,
				Length: length,
			}
			r.choices = append(
				r.choices,
				massageChoice{label: r.quote(length), action: massageButtonAction{Command: &command}},
			)
		} else {
			state := r.state
			state.View, state.Length, state.Page = massageProviders, length, 0
			r.view(r.quote(length), state)
		}
	}
	return nil
}

func (r *massageRenderer) selection(ctx context.Context) error {
	available, err := r.bot.API.MassageSlots(ctx, r.owner, r.state.Event, r.state.Party, r.state.Length)
	if err != nil {
		return err
	}
	if r.state.View == massageProviders {
		r.lines = append(r.lines, r.text(i18n.MassageSpecialist))
		providers := []massage.PublicProvider{}
		for _, provider := range available.Providers {
			if r.state.Length >= provider.MinLength && r.state.Length <= provider.MaxLength {
				providers = append(providers, provider)
			}
		}
		first, last := r.page(len(providers), massageChoicePageSize)
		for _, provider := range providers[first:last] {
			state := r.state
			state.View, state.Specialist, state.Page = "slots", provider.Owner, 0
			r.view(massageName(provider.Name), state)
		}
		return nil
	}
	r.lines = append(r.lines, r.text(i18n.MassageSlots)+"\n"+r.quote(r.state.Length))
	slots := []massage.SlotChoice{}
	for _, slot := range available.Slots {
		if slot.Specialist == r.state.Specialist {
			slots = append(slots, slot)
		}
	}
	first, last := r.page(len(slots), massageChoicePageSize)
	for _, slot := range slots[first:last] {
		command := massage.Command{
			Action:     "book",
			Event:      r.state.Event,
			Party:      r.state.Party,
			Specialist: slot.Specialist,
			Length:     r.state.Length,
			Slot:       slot.Slot,
		}
		r.choices = append(
			r.choices,
			massageChoice{label: massageWhen(slot.Start), action: massageButtonAction{Command: &command}},
		)
	}
	return nil
}

func (r *massageRenderer) instant(ctx context.Context) error {
	if _, err := r.bot.API.MassagePreferences(ctx, r.owner, r.state.Event); err != nil {
		return err
	}
	parties, err := r.bot.API.MassageParties(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, party := range parties {
		if now.After(party.Start.Add(-massagePartyTolerance)) && now.Before(party.End.Add(massagePartyTolerance)) {
			r.state.Party = party.ID
			return r.lengths(true)
		}
	}
	return nil
}

func (r *massageRenderer) preferences(ctx context.Context) error {
	prefs, err := r.bot.API.MassagePreferences(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	r.lines = append(r.lines, r.text(i18n.MassagePreferences))
	for _, bookings := range []bool{true, false} {
		id, enabled := i18n.MassageNextNotices, prefs.Next
		next := prefs
		if bookings {
			id, enabled = i18n.MassageBookingNotices, prefs.Bookings
			next.Bookings = !enabled
		} else {
			next.Next = !enabled
		}
		status := i18n.MassageOff
		if enabled {
			status = i18n.MassageOn
		}
		r.choices = append(
			r.choices,
			massageChoice{label: r.text(id) + ": " + r.text(status), action: massageButtonAction{Preferences: &next}},
		)
	}
	return nil
}

func (r *massageRenderer) bookings(ctx context.Context) error {
	bookings, err := r.readBookings(ctx)
	if err != nil {
		return err
	}
	names, err := r.bookingNames(ctx)
	if err != nil {
		return err
	}
	first, last := r.page(len(bookings), massageBookingPageSize)
	for _, booking := range bookings[first:last] {
		line := massageWhen(
			booking.Start,
		) + " · " + r.reservationQuote(
			booking,
		) + "\n" + massageName(
			booking.Specialist,
		)
		if name := names[booking.Specialist]; name != "" {
			line = massageWhen(booking.Start) + " · " + r.reservationQuote(booking) + "\n" + name
		}
		if r.state.View != "mine" {
			line += " · " + names[booking.Owner]
			if link := r.clientLinks[booking.Owner]; link != "" {
				r.choices = append(r.choices, massageChoice{label: names[booking.Owner], url: link})
			}
		}
		if booking.CancelledAt != nil {
			line += " · " + r.text(i18n.MassageCancelled)
		}
		r.lines = append(r.lines, line)
		if booking.Owner == r.owner && booking.CancelledAt == nil {
			command := massage.Command{
				Action:  mediaCancel,
				Event:   booking.Event,
				Booking: booking.ID,
				Version: booking.Version,
			}
			r.choices = append(
				r.choices,
				massageChoice{
					label:  r.text(i18n.MassageCancel) + " " + massageWhen(booking.Start),
					action: massageButtonAction{Command: &command},
				},
			)
		}
	}
	return nil
}

const massageChoicePageSize = 8
const massageBookingPageSize = 5
const massagePartyTolerance = 2 * time.Hour

func (r *massageRenderer) bookingNames(ctx context.Context) (map[string]string, error) {
	names := map[string]string{}
	if r.state.View == massageMine {
		providers, queryErr := r.bot.API.MassageProviderNames(ctx, r.owner, r.state.Event)
		if queryErr != nil {
			return nil, queryErr
		}
		for owner, name := range providers {
			names[owner] = massageName(name)
		}
	}
	if r.state.View != "mine" {
		calendar, calendarErr := r.bot.API.MassageTimetable(ctx, r.owner, r.state.Event)
		if calendarErr != nil {
			return nil, calendarErr
		}
		for _, client := range calendar.Clients {
			names[client.Owner] = massageName(client.Name)
			if r.clientLinks == nil {
				r.clientLinks = map[string]string{}
			}
			r.clientLinks[client.Owner] = "tg://user?id=" + strconv.FormatInt(client.TelegramID, 10)
		}
		for _, provider := range calendar.Providers {
			names[provider.Owner] = massageName(provider.Name)
		}
	}
	return names, nil
}
