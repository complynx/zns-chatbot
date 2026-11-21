package task

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/passcopy"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	passActionRole     = "role"
	passActionMode     = "mode"
	passActionJoin     = "join"
	passActionEvent    = "event"
	passActionReplace  = "replace"
	passActionComplete = "complete"
	passInvitePaired   = "entry.invite_paired"
)

type passEntry struct {
	Input   passInput             `json:"input"`
	Prefill *passPrefill          `json:"prefill,omitempty"`
	Event   *db.RegistrationEntry `json:"event,omitempty"`
	Choices []passChoice          `json:"choices,omitempty"`
}

type passPrefill struct {
	Origin  uuid.UUID `json:"origin"`
	Version int64     `json:"version"`
}

type passChoice struct {
	Action  string    `json:"action"`
	ID      uuid.UUID `json:"id,omitempty"`
	Version int64     `json:"version,omitempty"`
	Value   string    `json:"value,omitempty"`
}

type passScreen struct {
	entry *passEntry
	rows  [][]telegram.InlineKeyboardButton
	id    uuid.UUID
}

func (p *passScreen) button(label string, choice passChoice) telegram.InlineKeyboardButton {
	data := passCallbackPrefix + p.id.String() + ":" + strconv.Itoa(len(p.entry.Choices))
	p.entry.Choices = append(p.entry.Choices, choice)
	return telegram.InlineKeyboardButton{Text: label, CallbackData: data}
}

func (p *passScreen) add(label string, choice passChoice) {
	p.rows = append(p.rows, []telegram.InlineKeyboardButton{p.button(label, choice)})
}

func passText(ru bool, key string, args ...any) string {
	return passcopy.Text(structuredText(ru, "en", "ru"), key, args...)
}

func (s *Service) structuredPasses(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
) error {
	entry, choice, err := s.passEntryInput(ctx, *t, i, scope)
	if errors.Is(err, db.ErrRoutingPending) {
		return s.wait()
	}
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	screen := passScreen{id: t.ID, entry: &entry}
	c.PassEntry = &entry
	payment := db.PaymentScope{EventScope: registrationContextScope(scope).EventScope,
		UserID: scope.UserID, Subject: scope.Subject}
	input := &entry.Input
	if input.Invalid {
		if input.Code != "" {
			return s.finishPassScreen(ctx, t, c, i, ru, &screen, passText(ru, "entry.invite_unavailable"))
		}
		return s.passEntryError(ctx, t, c, i, ru, registrations.ErrInvalid)
	}
	if input.DetailsDisabled {
		return s.finishPassScreen(ctx, t, c, i, ru, &screen, passText(ru, "entry.details_unavailable"))
	}
	if err = s.bindPassPrefill(ctx, *t, c, i, scope, &entry); err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	switch choice.Action {
	case "list":
		return s.passEvents(ctx, t, c, i, scope, ru, &screen, choice.ID)
	case passActionRole:
		input.Role = choice.Value
	case passActionMode:
		input.Mode = choice.Value
	case passActionJoin:
		return s.finishPassScreen(ctx, t, c, i, ru, &screen, passText(ru, "entry.invite_usage"))
	case "leave_confirm":
		screen.add(passText(ru, "entry.leave_pair"), passChoice{Action: "leave", ID: choice.ID, Version: choice.Version})
		return s.finishPassScreen(ctx, t, c, i, ru, &screen, passText(ru, "entry.leave_confirm"))
	case "leave":
		_, err = s.store.DissolvePair(ctx, payment, passOperation(i, t.ID), db.PairDissolution{
			EventID: entry.Event.EventID, PairID: choice.ID, ExpectedVersion: choice.Version,
		})
		return s.passSimpleResult(ctx, t, c, i, ru, &screen, "entry.pair_left", err)
	case "revoke":
		err = s.store.RevokePassInvite(ctx, payment, entry.Event.EventID, choice.Value)
		return s.passSimpleResult(ctx, t, c, i, ru, &screen, "entry.invite_revoked", err)
	}
	eventID := choice.ID
	if entry.Event != nil {
		eventID = entry.Event.EventID
	}
	if eventID == uuid.Nil && input.Event == "" && input.Code == "" {
		return s.passEvents(ctx, t, c, i, scope, ru, &screen, uuid.Nil)
	}
	return s.passSelectedEvent(ctx, t, c, i, scope, payment, ru, &screen, choice, eventID)
}

func (s *Service) passSelectedEvent(
	ctx context.Context, t *db.Task, c checkpoint, i intent, scope db.ProfileScope, payment db.PaymentScope,
	ru bool, screen *passScreen, choice passChoice, eventID uuid.UUID,
) error {
	entry := screen.entry
	input := &entry.Input
	event, now, err := s.resolvePassEntryEvent(ctx, scope, payment, input, eventID)
	if input.Code != "" && passChoiceUnavailable(err) {
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, "entry.invite_unavailable"))
	}
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	if entry.Event == nil {
		entry.Event = &db.RegistrationEntry{EventID: event.ID, EventVersion: event.Version,
			ExpiresAt: now.Add(registrationChoiceLifetime)}
	}
	if len(input.Details) != 0 && (event.Version != entry.Event.EventVersion || !now.Before(entry.Event.ExpiresAt)) {
		return s.passEntryError(ctx, t, c, i, ru, db.ErrConflict)
	}
	if choice.Action == passActionReplace || choice.Action == "issue" {
		replacing := uuid.Nil
		if choice.Action == passActionReplace {
			replacing = choice.ID
		}
		invite, issueErr := s.store.IssuePassInvite(ctx, payment, event.ID, passOperation(i, t.ID), replacing)
		return s.passInviteResult(ctx, t, c, i, ru, screen, event, invite, replacing != uuid.Nil, issueErr)
	}
	if input.Code == "" && choice.Action != passActionComplete {
		handled, menuErr := s.currentPassEntryMenu(ctx, t, c, i, scope, payment, ru, screen, event, now)
		if handled {
			return menuErr
		}
	}
	if unavailable := passSaleUnavailable(ru, event, now); unavailable != "" {
		return s.finishPassScreen(ctx, t, c, i, ru, screen, unavailable)
	}
	rules := event.Document.Registration
	if event.Version != entry.Event.EventVersion {
		return s.passEntryError(ctx, t, c, i, ru, db.ErrConflict)
	}
	if rules.ClosesAt.Before(entry.Event.ExpiresAt) {
		entry.Event.ExpiresAt = rules.ClosesAt
	}
	return s.passOpenRegistration(ctx, t, c, i, scope, payment, ru, screen, choice, event, now)
}

func (s *Service) passOpenRegistration(
	ctx context.Context, t *db.Task, c checkpoint, i intent, scope db.ProfileScope, payment db.PaymentScope,
	ru bool, screen *passScreen, choice passChoice, event events.Event, now time.Time,
) error {
	input := &screen.entry.Input
	intro := ""
	if input.Mode != passModePair && input.Code == "" {
		priority, err := s.store.PassTicketPriority(ctx, payment, event.ID, input.Role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return s.passEntryError(ctx, t, c, i, ru, err)
		}
		if err == nil && now.Before(priority.ExpiresAt) {
			intro = passText(ru, "queue.promoDeadline", passLocalTime(priority.ExpiresAt, event.Document.Timezone))
		}
	}
	paired := false
	if input.Code != "" {
		invite, err := s.store.ReservedPassInvite(ctx, payment, event.ID, input.Code, input.Role)
		if err != nil {
			return s.passInviteResult(ctx, t, c, i, ru, screen, event, invite, false, err)
		}
		paired = invite.PairID != nil
		input.Role = invite.Role
		intro = passText(ru, "entry.invite_claimed", passText(ru, "entry."+input.Role))
	}
	if err := s.storePassInputDetails(ctx, t, &c, i, scope, input); err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	if paired {
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, passInvitePaired))
	}
	if input.Role == "" {
		screen.rows = append(screen.rows, []telegram.InlineKeyboardButton{
			screen.button(passText(ru, "entry.leader"), passChoice{Action: passActionRole, Value: registrations.Leader}),
			screen.button(passText(ru, "entry.follower"), passChoice{Action: passActionRole, Value: registrations.Follower}),
		})
		text := passText(ru, "entry.pick_role", event.Document.Name)
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passTextParts(intro, text))
	}
	if input.Mode == "" && input.Code == "" && choice.Action == passActionRole {
		screen.add(passText(ru, "entry.solo"), passChoice{Action: passActionMode, Value: "solo"})
		screen.add(passText(ru, "entry.create_pair"), passChoice{Action: passActionMode, Value: passModePair})
		screen.add(passText(ru, "entry.join_pair"), passChoice{Action: passActionJoin})
		return s.finishPassScreen(ctx, t, c, i, ru, screen,
			passTextParts(intro, passText(ru, "entry.pick_mode", event.Document.Name)))
	}
	return s.completePassEntry(ctx, t, c, i, scope, payment, ru, screen, event, intro)
}

func (s *Service) passEntryInput(
	ctx context.Context,
	t db.Task,
	i intent,
	scope db.ProfileScope,
) (passEntry, passChoice, error) {
	var entry passEntry
	if i.PassCallback == "" {
		if i.PassInput != nil {
			entry.Input = *i.PassInput
		}
		return entry, passChoice{}, nil
	}
	control := passCallbackPrefix + i.CallbackTaskID.String() + ":" + i.PassCallback
	raw, err := s.store.PassEntryPrompt(ctx, scope, t.ID, i.CallbackTaskID, i.CallbackMessageID, control)
	if err != nil {
		return entry, passChoice{}, err
	}
	if err = json.Unmarshal(raw, &entry); err != nil {
		return entry, passChoice{}, err
	}
	index, err := strconv.Atoi(i.PassCallback)
	if err != nil || index < 0 || index >= len(entry.Choices) {
		return entry, passChoice{}, pgx.ErrNoRows
	}
	choice := entry.Choices[index]
	entry.Choices = nil
	return entry, choice, nil
}

func (s *Service) resolvePassEntryEvent(
	ctx context.Context, scope db.ProfileScope, payment db.PaymentScope, input *passInput, id uuid.UUID,
) (events.Event, time.Time, error) {
	if id == uuid.Nil && input.Event != "" {
		parsed, err := uuid.Parse(input.Event)
		if err != nil || parsed == uuid.Nil || parsed.String() != input.Event {
			// An older, unbound request must not follow a reassigned slug.
			return events.Event{}, time.Time{}, db.ErrConflict
		}
		id = parsed
	}
	if id == uuid.Nil {
		invited, err := s.store.PassInviteEvent(ctx, payment, input.Code)
		if err != nil {
			return events.Event{}, time.Time{}, err
		}
		id = invited
	}
	return s.store.PassEntryEvent(ctx, registrationContextScope(scope), id)
}

func (s *Service) passEvents(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
	screen *passScreen,
	after uuid.UUID,
) error {
	page, err := s.store.ListEventContexts(
		ctx,
		registrationContextScope(scope),
		"registrations",
		after,
		registrationEventPageSize,
	)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	for _, event := range page.Events {
		screen.add(event.Name, passChoice{Action: passActionEvent, ID: event.ID})
	}
	if page.Next != nil {
		screen.add(passText(ru, "entry.next"), passChoice{Action: "list", ID: *page.Next})
	}
	key := "entry.pick_event"
	if len(page.Events) == 0 {
		key = "entry.no_events"
	}
	return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, key))
}

func (s *Service) finishPassScreen(
	ctx context.Context, t *db.Task, c checkpoint, i intent, ru bool, screen *passScreen, text string,
) error {
	c.RegistrationEntry = screen.entry.Event
	if len(screen.rows) == 0 {
		c.PassEntry = nil
	}
	return s.finishRegistration(ctx, t, c, i, text,
		&telegram.InlineKeyboardMarkup{InlineKeyboard: screen.rows}, ru)
}

func (s *Service) passEntryError(ctx context.Context, t *db.Task, c checkpoint, i intent, ru bool, err error) error {
	c.PassEntry = nil
	if passChoiceUnavailable(err) {
		return s.finishRegistration(ctx, t, c, i, passText(ru, "entry.invalid"), nil, ru)
	}
	return s.identityError(ctx, t, c, i, err)
}

func passChoiceUnavailable(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, db.ErrConflict) ||
		errors.Is(err, registrations.ErrInvalid) || errors.Is(err, registrations.ErrUnavailable)
}

func passOperation(i intent, taskID uuid.UUID) string {
	if i.PassCallback == "" {
		return "passes:" + taskID.String()
	}
	return "passes:" + i.CallbackTaskID.String() + ":" + i.PassCallback
}

func passLocalTime(at time.Time, timezone string) string {
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return at.UTC().Format("2006-01-02 15:04") + " (UTC)"
	}
	return at.In(zone).Format("2006-01-02 15:04") + " (" + timezone + ")"
}

func passTextParts(first, second string) string {
	return strings.TrimSpace(first + "\n\n" + second)
}
