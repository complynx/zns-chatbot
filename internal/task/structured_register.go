package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	registrationChoiceLifetime    = 2 * time.Hour
	registrationEventPageSize     = 5
	registrationEventCommandParts = 2
	registrationReadCallbackParts = 2
)

func registrationCallback(i intent, cb *telegram.CallbackQuery) (intent, bool, error) {
	parts := strings.Split(cb.Data, ":")
	if len(parts) == 0 || (parts[0] != "e" && parts[0] != "j" && parts[0] != "g") {
		return i, false, nil
	}
	if len(cb.Data) > 64 || len(parts) < 2 {
		return i, true, ErrUnsupportedUpdate
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil || id.String() != parts[1] {
		return i, true, ErrUnsupportedUpdate
	}
	i.Kind, i.StructuredCommand = kindStructured, registerCommand
	i.CallbackMessageID = cb.Message.ID
	switch parts[0] {
	case "e", "j":
		if len(parts) != registrationReadCallbackParts {
			return i, true, ErrUnsupportedUpdate
		}
		i.Text = registerCommand + " " + id.String()
		if parts[0] == "j" {
			i.Text = registerCommand + " after " + id.String()
		}
	case "g":
		if len(parts) != 3 || !registrations.ValidRole(parts[2]) {
			return i, true, ErrUnsupportedUpdate
		}
		i.RegistrationPromptID, i.RegistrationRole = id, parts[2]
		i.Text = registerCommand
	}
	return i, true, nil
}

func registrationContextScope(scope db.ProfileScope) db.EventContextScope {
	eventScope := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	return db.EventContextScope{
		EventScope: eventScope,
		UserID:     scope.UserID,
		Subject:    scope.Subject,
	}
}

func (s *Service) structuredRegister(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
) error {
	if i.RegistrationPromptID != uuid.Nil {
		return s.registerChoice(ctx, t, c, i, scope, ru)
	}
	args := strings.Fields(i.Text)
	if len(args) == 1 {
		return s.registrationEventsMenu(ctx, t, c, i, scope, uuid.Nil, ru)
	}
	var raw string
	page := false
	switch {
	case len(args) == registrationEventCommandParts:
		raw = args[1]
	case len(args) == 3 && args[1] == "after":
		raw = args[2]
		page = true
	default:
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	if page {
		return s.registrationEventsMenu(ctx, t, c, i, scope, id, ru)
	}
	return s.registrationRoleMenu(ctx, t, c, i, scope, id, ru)
}

func (s *Service) registrationRoleMenu(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	id uuid.UUID,
	ru bool,
) error {
	eventScope := registrationContextScope(scope)
	selected, err := s.store.ResolveEventContext(
		ctx,
		eventScope,
		events.ContextRequest{Module: "registrations", EventID: &id, RequireExplicit: true},
		"",
	)
	if errors.Is(err, events.ErrContextUnavailable) || errors.Is(err, pgx.ErrNoRows) {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	event := selected.Selected
	if event == nil || event.Registration == nil {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	expiry := time.Now().UTC().Add(registrationChoiceLifetime)
	if event.Registration.ClosesAt.Before(expiry) {
		expiry = event.Registration.ClosesAt
	}
	c.RegistrationEntry = &db.RegistrationEntry{EventID: event.ID, EventVersion: event.Version, ExpiresAt: expiry}
	zone, err := time.LoadLocation(event.Timezone)
	if err != nil {
		return err
	}
	text := event.Name + "\n" + event.StartsAt.In(zone).Format("2006-01-02 15:04") + " (" + event.Timezone + ")"
	text += structuredText(
		ru,
		"\nChoose your role to join the waiting list. This does not assign or pay for a pass.\nChoices expire: ",
		"\nВыбери роль для заявки в лист ожидания. Это не назначает пасс и не подтверждает оплату.\nВыбор действует до: ",
	) + expiry.In(zone).
		Format("2006-01-02 15:04") +
		" (" + event.Timezone + ")"
	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: structuredText(ru, "Leader", "Лидер"), CallbackData: "g:" + t.ID.String() + ":leader"},
				{
					Text:         structuredText(ru, "Follower", "Фолловер"),
					CallbackData: "g:" + t.ID.String() + ":follower",
				},
			},
		},
	}
	return s.finishRegistration(ctx, t, c, i, text, keyboard, ru)
}

func (s *Service) registrationEventsMenu(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	after uuid.UUID,
	ru bool,
) error {
	eventScope := registrationContextScope(scope)
	page, err := s.store.ListEventContexts(ctx, eventScope, "registrations", after, registrationEventPageSize)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	text := structuredText(ru, "Choose an event for registration:", "Выбери мероприятие для регистрации:")
	keyboard := &telegram.InlineKeyboardMarkup{}
	for _, event := range page.Events {
		keyboard.InlineKeyboard = append(
			keyboard.InlineKeyboard,
			[]telegram.InlineKeyboardButton{{Text: event.Name, CallbackData: "e:" + event.ID.String()}},
		)
	}
	if page.Next != nil {
		keyboard.InlineKeyboard = append(
			keyboard.InlineKeyboard,
			[]telegram.InlineKeyboardButton{
				{Text: structuredText(ru, "Next page", "Следующая страница"), CallbackData: "j:" + page.Next.String()},
			},
		)
	}
	if len(page.Events) == 0 {
		text = structuredText(ru, "No open registrations on this page.", "На этой странице нет открытых регистраций.")
	}
	return s.finishRegistration(ctx, t, c, i, text, keyboard, ru)
}

func (s *Service) registerChoice(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
) error {
	if c.RegistrationChange == nil {
		entry, err := s.store.RegistrationEntryForCallback(
			ctx,
			scope,
			t.ID,
			i.RegistrationPromptID,
			i.CallbackMessageID,
			i.RegistrationRole,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return s.registrationRetry(ctx, t, c, i, ru)
		}
		if err != nil {
			return s.identityError(ctx, t, c, i, err)
		}
		c.RegistrationEntry = &entry
		c.RegistrationChange = &db.RegistrationChange{
			Kind:    "register",
			EventID: entry.EventID,
			UserID:  scope.UserID,
			Role:    i.RegistrationRole,
		}
		c.RegistrationActor = scope.Subject
		c.RegistrationOperation = "register:" + i.RegistrationPromptID.String() + ":" + i.RegistrationRole
		if err = s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
	}
	if c.RegistrationEntry == nil || c.RegistrationActor != scope.Subject {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	eventScope := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	selection := *c.RegistrationEntry
	selection.DetailsOperation = t.ID
	result, err := s.store.RegisterPresented(
		ctx,
		eventScope,
		c.RegistrationActor,
		c.RegistrationOperation,
		*c.RegistrationChange,
		selection,
	)
	if errors.Is(err, registrationdetails.ErrRequired) {
		return s.registrationDetailsEntry(ctx, t, c, i, scope, ru)
	}
	if errors.Is(err, db.ErrConflict) || errors.Is(err, registrations.ErrUnavailable) || errors.Is(err, pgx.ErrNoRows) {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	// A repeated click can follow an administrator assignment; display current state.
	result, err = s.store.Registration(ctx, eventScope, result.EventID, scope.UserID)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	text := fmt.Sprintf(
		"%s\n%s: %s\n/registrations",
		structuredText(ru, "Registration saved", "Регистрация сохранена"),
		structuredText(ru, "Role", "Роль"),
		structuredRole(result.Role, ru),
	)
	if result.State == registrations.Assigned {
		text += structuredText(ru, "\nPass assigned.", "\nПасс назначен.")
	} else {
		text += structuredText(
			ru,
			"\nYou are on the waiting list. No pass or payment was created.",
			"\nТы в листе ожидания. Пасс и платёж не создавались.",
		)
	}
	return s.finishRegistration(ctx, t, c, i, text, nil, ru)
}

func (s *Service) registrationRetry(ctx context.Context, t *db.Task, c checkpoint, i intent, ru bool) error {
	return s.finishRegistration(
		ctx,
		t,
		c,
		i,
		structuredText(
			ru,
			"The registration choice could not be verified, changed, or expired. Open /register again; "+
				"an existing role cannot be changed here.",
			"Не удалось проверить выбор, он изменился или истёк. Открой /register заново; "+
				"изменить существующую роль здесь нельзя.",
		),
		nil,
		ru,
	)
}

func (s *Service) finishRegistration(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	text string,
	keyboard *telegram.InlineKeyboardMarkup,
	ru bool,
) error {
	out := structuredOutput(i, text, nil, ru, "")
	if keyboard != nil && len(keyboard.InlineKeyboard) > 0 {
		var payload telegram.SendMessageRequest
		if err := json.Unmarshal(out[len(out)-1].Payload, &payload); err != nil {
			return err
		}
		payload.ReplyMarkup = keyboard
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		out[len(out)-1].Payload = raw
	}
	t.State = stateCompleted
	if err := s.save(ctx, t, c, out); err != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}
