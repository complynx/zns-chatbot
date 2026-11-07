package task

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// The verified role click creates an expectation, never an automatic registration.
// The user returns to a fresh role choice after completing the shared fields.
const detailsRequestPending = "pending"

func (s *Service) registrationDetailsEntry(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
) error {
	request, err := s.store.BeginDetailsRequest(ctx, scope, t.ID, db.DetailsRequestStart{
		EventID: c.RegistrationEntry.EventID, ExpectedEventVersion: c.RegistrationEntry.EventVersion,
	})
	if errors.Is(err, db.ErrConflict) || errors.Is(err, registrations.ErrUnavailable) || errors.Is(err, pgx.ErrNoRows) {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	// A begin receipt is historical. Render the current expectation after a retry.
	request, err = s.store.DetailsRequest(ctx, scope, request.Request.ID)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	if request.State != detailsRequestPending && request.State != "completed" {
		return s.registrationRetry(ctx, t, c, i, ru)
	}
	fields := make([]string, 0, len(request.MissingFields))
	for _, field := range request.MissingFields {
		switch field {
		case registrationdetails.LegalName:
			fields = append(fields, structuredText(ru, "legal full name", "ФИО по документу"))
		case registrationdetails.PassportNumber:
			fields = append(fields, structuredText(ru, "passport number", "номер паспорта"))
		}
	}
	text := structuredText(
		ru,
		"Before registering, fill in: ",
		"Перед регистрацией заполни: ",
	) + strings.Join(
		fields,
		", ",
	)
	if len(fields) == 0 {
		text = structuredText(
			ru,
			"Required details are saved. Return to role selection to register.",
			"Необходимые данные сохранены. Вернись к выбору роли для регистрации.",
		)
	}
	zone, err := time.LoadLocation(request.Request.EventTimezone)
	if err != nil {
		return err
	}
	text += structuredText(
		ru,
		"\nThis data request expires: ",
		"\nЗапрос данных действует до: ",
	) + request.Request.ExpiresAt.In(zone).
		Format("2006-01-02 15:04") +
		" (" + request.Request.EventTimezone + ")"
	text += structuredText(
		ru,
		"\nSaving details does not register you. After saving, return here and choose a role again.",
		"\nСохранение данных не создаёт регистрацию. После сохранения вернись сюда и выбери роль заново.",
	)
	keyboard := &telegram.InlineKeyboardMarkup{}
	if len(fields) > 0 && s.options.RegistrationDetailsEnabled && s.options.MiniAppURL != "" {
		keyboard.InlineKeyboard = append(
			keyboard.InlineKeyboard,
			[]telegram.InlineKeyboardButton{
				{
					Text:   structuredText(ru, "Fill in registration details", "Заполнить данные"),
					WebApp: &telegram.WebAppInfo{URL: s.options.MiniAppURL},
				},
			},
		)
	} else if len(fields) > 0 {
		text += structuredText(
			ru,
			"\nThe form is currently unavailable. Try /register later or ask me to help fill in these fields.",
			"\nФорма сейчас недоступна. Попробуй /register позже или попроси меня помочь заполнить данные.",
		)
	}
	keyboard.InlineKeyboard = append(
		keyboard.InlineKeyboard,
		[]telegram.InlineKeyboardButton{
			{
				Text:         structuredText(ru, "Return to role selection", "Вернуться к выбору роли"),
				CallbackData: "e:" + request.Request.EventID.String(),
			},
		},
	)
	return s.finishRegistration(ctx, t, c, i, text, keyboard, ru)
}
