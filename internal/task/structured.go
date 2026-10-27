package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	registrationPageCallback = "r:"
	startCommand             = "/start"
	avatarCommand            = "/avatar"
	pairsCommand             = "/pairs"
	menuCommand              = "/menu"
	ordersCommand            = "/orders"
	massageCommand           = "/massage"
	knowledgeCommand         = "/knowledge"
	paymentReviewsCommand    = "/payment_reviews"
	paymentsCommand          = "/payments"
	nameCommand              = "/name"
	registerCommand          = "/register"
	profileCommand           = "/profile"
	registrationsCommand     = "/registrations"
	registrationCommandParts = 2
)

const structuredWelcomeLeadEN = "Hi! I’m ZiNuSya, your assistant for the Zouk Non Stop community and events."

const structuredWelcomeLeadRU = "Привет! Я ЗиНуСя, помощница сообщества и мероприятий Zouk Non Stop."

const structuredWelcomeEN = structuredWelcomeLeadEN + " " +
	"Write what you need, or choose an available service below.\n"

const structuredWelcomeRU = structuredWelcomeLeadRU + " " +
	"Напиши, что тебе нужно, или выбери доступный сервис ниже.\n"

func structuredCommand(text string) bool {
	text = strings.TrimSpace(text)
	return text == registerCommand || strings.HasPrefix(text, registerCommand+" ") ||
		text == startCommand || text == pairsCommand || text == avatarCommand || text == menuCommand ||
		text == ordersCommand || text == massageCommand || text == paymentReviewsCommand ||
		text == paymentsCommand || text == profileCommand ||
		text == registrationsCommand ||
		strings.HasPrefix(text, registrationsCommand+" ")
}

func structuredText(ru bool, english, russian string) string {
	if ru {
		return russian
	}
	return english
}

// structured uses the normal durable task/outbox, without touching Loom or its active task.
func (s *Service) structured(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	user, _, err := s.authorize(ctx, *t, i)
	var principal identity.Principal
	if err == nil {
		principal, err = s.taskPrincipal(ctx, user)
	}
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	s.refreshMenuQuietly(ctx, i.ChatID)
	scope := db.ProfileScope{
		BotID:        s.options.BotID,
		Organization: user.Organization,
		Subject:      user.Subject,
		UserID:       user.ID,
	}
	ru := s.languageFor(ctx, i) == "ru"
	if i.StructuredCommand == registerCommand {
		return s.structuredRegister(ctx, t, c, i, scope, ru)
	}
	// Only already-admitted native name tasks can reach this branch. New /name
	// messages use the agent dialogue and explicit profile tools.
	if i.StructuredCommand == nameCommand {
		return s.structuredName(ctx, t, c, i, scope, ru)
	}
	if i.StructuredCommand == paymentReviewsCommand {
		return s.structuredPaymentReviews(ctx, t, c, i, s.taskPaymentScope(user, principal), ru)
	}
	text, next, err := s.structuredView(ctx, scope, strings.TrimSpace(i.Text), ru)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	formURL := ""
	if s.options.MiniAppURL != "" {
		switch i.StructuredCommand {
		case menuCommand, startCommand:
			formURL = s.options.MiniAppURL
		case paymentsCommand:
			formURL = s.options.MiniAppURL + "#pass-payments"
		case pairsCommand:
			formURL = s.options.MiniAppURL + "#pairs"
		case avatarCommand:
			formURL = s.options.MiniAppURL + "#avatars"
		case ordersCommand:
			formURL = s.options.MiniAppURL + "#orders"
		case massageCommand:
			formURL = s.options.MiniAppURL + "#massage"
		case knowledgeCommand:
			formURL = s.options.MiniAppURL + "#knowledge"
		}
	}
	t.State = stateCompleted
	out := structuredOutput(i, text, next, ru, formURL)
	if i.StructuredCommand == menuCommand || i.StructuredCommand == startCommand {
		out = structuredMenuOutput(i, text, ru, formURL)
	}
	if err = s.save(ctx, t, c, out); err != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}

func (s *Service) taskPrincipal(ctx context.Context, user db.User) (identity.Principal, error) {
	bot, organization, _ := s.iam.DeploymentScope()
	if user.Revoked || user.BotID != bot || user.Organization != organization || user.Subject == "" {
		return identity.Principal{}, identity.ErrDenied
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return identity.Principal{}, err
	}
	principal, err := s.iam.Verify(ctx, credential.Token)
	if err != nil {
		return identity.Principal{}, err
	}
	if principal.Subject != user.Subject || principal.OrganizationID != organization {
		return identity.Principal{}, identity.ErrDenied
	}
	return principal, nil
}

func (s *Service) structuredView(
	ctx context.Context,
	scope db.ProfileScope,
	command string,
	ru bool,
) (string, *uuid.UUID, error) {
	switch command {
	case menuCommand, startCommand:
		text, err := s.menuText(ctx, scope, ru)
		return text, nil, err
	case paymentsCommand:
		return s.structuredPaymentsView(ru), nil, nil
	case pairsCommand:
		if s.options.MiniAppURL == "" {
			return structuredText(ru,
				"The pair invitation form is currently unavailable. Try /pairs later.",
				"Форма приглашений в пару сейчас недоступна. Попробуй /pairs позже."), nil, nil
		}
		return structuredText(
			ru,
			"Open pair invitations to share your partner code, invite or respond. "+
				"Opening the form does not create a pair or give consent.",
			"Открой приглашения в пару, чтобы передать свой код, пригласить или ответить. "+
				"Открытие формы не создаёт пару и не означает согласия.",
		), nil, nil
	case avatarCommand:
		if s.options.MiniAppURL == "" {
			return structuredText(ru,
				"The avatar form is currently unavailable. Try /avatar later.",
				"Форма аватарки сейчас недоступна. Попробуй /avatar позже."), nil, nil
		}
		return structuredText(
			ru,
			"Open the form to upload an original, adjust the crop and save your avatar. "+
				"Opening the form does not change your avatar.",
			"Открой форму, загрузи оригинал, настрой обрезку и сохрани аватарку. Открытие формы не меняет аватарку.",
		), nil, nil
	case ordersCommand:
		if s.options.MiniAppURL == "" {
			return structuredText(ru,
				"The saved-selection form is currently unavailable. Try /orders later.",
				"Форма сохранённого выбора сейчас недоступна. Попробуй /orders позже."), nil, nil
		}
		return structuredText(
			ru,
			"Open the form to choose an event and save or change quantities. Saving does not place an order or reserve items.",
			"Открой форму, выбери эвент и сохрани или измени количество. Сохранение не оформляет заказ и не резервирует товары.",
		), nil, nil
	case massageCommand:
		if s.options.MiniAppURL == "" {
			return structuredText(ru,
				"The massage form is currently unavailable. Try /massage later.",
				"Форма массажа сейчас недоступна. Попробуй /massage позже."), nil, nil
		}
		return structuredText(
			ru,
			"Open the form to choose an event and review a massage slot or cancel your booking. "+
				"A preview does not reserve a slot or confirm payment.",
			"Открой форму, выбери эвент и проверь время массажа или отмени свою запись. "+
				"Предпросмотр не занимает место и не подтверждает оплату.",
		), nil, nil
	case knowledgeCommand:
		return s.structuredKnowledgeView(ru), nil, nil
	case profileCommand:
		text, err := s.structuredProfileView(ctx, scope, ru)
		return text, nil, err
	}
	after, valid := registrationCursor(command)
	if !valid {
		return structuredText(ru, "Open /registrations again.", "Открой /registrations заново."), nil, nil
	}
	page, err := s.store.OwnAccountEvents(ctx, scope, after, 0)
	if err != nil {
		return "", nil, err
	}
	text := structuredText(ru, "My registrations and passes", "Мои регистрации и пассы")
	if len(page.Events) == 0 {
		text += structuredText(ru, "\nNo registrations on this page.", "\nНа этой странице нет регистраций.")
	}
	var output strings.Builder
	output.WriteString(text)
	for _, event := range page.Events {
		output.WriteString("\n\n")
		output.WriteString(structuredEvent(event, ru))
	}
	text = output.String()
	if page.HasMore {
		text += structuredText(
			ru,
			"\n\nMore registrations are on the next page.",
			"\n\nЕщё регистрации — на следующей странице.",
		)
	}
	return text, page.Next, nil
}

func structuredEvent(event db.AccountEvent, ru bool) string {
	role := structuredRole(event.Registration.Role, ru)
	state := structuredText(ru, "Waitlisted", "В очереди")
	if event.Registration.State == "assigned" {
		state = structuredText(ru, "Pass assigned", "Пасс назначен")
	}
	text := event.EventName + "\n" + role + " · " + state + "\n" + event.StartsAt.Format(
		"2006-01-02 15:04",
	) + " (" + event.Timezone + ")"
	if !event.RegistrationEnabled {
		return text + structuredText(
			ru,
			"\nRegistration module disabled; pass details unavailable.",
			"\nМодуль регистрации отключён; данные пасса недоступны.",
		)
	}
	if event.Pass == nil {
		return text
	}
	pass := event.Pass
	balance := pass.Balance
	text += structuredText(
		ru,
		"\nConfirmed: ",
		"\nПодтверждено: ",
	) + noticeAmount(
		balance.ConfirmedMinor,
		balance.CurrencyScale,
		balance.Currency,
	)
	text += structuredText(
		ru,
		"\nRemaining: ",
		"\nОсталось: ",
	) + noticeAmount(
		balance.RemainingMinor,
		balance.CurrencyScale,
		balance.Currency,
	)
	text += structuredPaymentDeadlines(*pass, event.Timezone, ru)
	if pass.PendingEvidence > 0 {
		text += fmt.Sprintf(
			structuredText(ru, "\nAwaiting payment review: %d", "\nПодтверждений оплаты на проверке: %d"),
			pass.PendingEvidence,
		)
	}
	return text
}

func structuredOutput(i intent, text string, next *uuid.UUID, ru bool, formURL string) []db.Outbound {
	keyboard := &telegram.ReplyKeyboardRemove{RemoveKeyboard: true}
	chunks := render.Plain(text)
	out := make([]db.Outbound, 0, len(chunks))
	for n, chunk := range chunks {
		payload := telegram.SendMessageRequest{ChatID: i.ChatID, Text: chunk.Text}
		if n == len(chunks)-1 {
			payload.ReplyMarkup = keyboard
			if formURL != "" {
				payload.ReplyMarkup = &telegram.InlineKeyboardMarkup{
					InlineKeyboard: [][]telegram.InlineKeyboardButton{{{
						Text:   structuredFormLabel(i.StructuredCommand, ru),
						WebApp: &telegram.WebAppInfo{URL: formURL},
					}}},
				}
			}
			if next != nil {
				payload.ReplyMarkup = &telegram.InlineKeyboardMarkup{
					InlineKeyboard: [][]telegram.InlineKeyboardButton{
						{
							{
								Text: structuredText(
									ru,
									"Next page",
									"Следующая страница",
								),
								CallbackData: registrationPageCallback + next.String(),
							},
						},
					},
				}
			}
		}
		raw, _ := json.Marshal(payload)
		out = append(out, db.Outbound{DedupKey: "result:structured:" + strconv.Itoa(n), Payload: raw})
	}
	return out
}

func registrationCursor(command string) (uuid.UUID, bool) {
	args := strings.Fields(command)
	if len(args) == 0 || args[0] != registrationsCommand || len(args) > registrationCommandParts {
		return uuid.Nil, false
	}
	if len(args) == 1 {
		return uuid.Nil, true
	}
	after, err := uuid.Parse(args[1])
	return after, err == nil && after != uuid.Nil && after.String() == args[1]
}

func structuredPaymentDeadlines(pass db.AccountPass, zone string, ru bool) string {
	balance := pass.Balance
	if balance.FullyPaid {
		return structuredText(ru, "\nPaid in full.", "\nПолностью оплачен.")
	}
	fullDeadline := pass.FinalDueAt.Format("2006-01-02 15:04") + " (" + zone + ")"
	splitPayment := balance.DepositRequiredMinor < balance.PriceMinor || !pass.InitialDueAt.Equal(pass.FinalDueAt)
	if !splitPayment {
		return structuredText(ru, "\nFull payment due: ", "\nСрок полной оплаты: ") + fullDeadline
	}
	text := structuredText(ru, "\nDeposit confirmed.", "\nПредоплата подтверждена.")
	if !balance.DepositSatisfied {
		outstanding := balance.DepositRequiredMinor - balance.ConfirmedMinor
		text = structuredText(
			ru,
			"\nRemaining to secure deposit: ",
			"\nДо предоплаты осталось: ",
		) + noticeAmount(
			outstanding,
			balance.CurrencyScale,
			balance.Currency,
		)
		text += structuredText(
			ru,
			"\nDeposit due: ",
			"\nСрок предоплаты: ",
		) + pass.InitialDueAt.Format(
			"2006-01-02 15:04",
		) + " (" + zone + ")"
	}
	return text + structuredText(ru, "\nFull payment due: ", "\nСрок полной оплаты: ") + fullDeadline
}

func structuredRole(role string, ru bool) string {
	if role == "follower" {
		return structuredText(ru, "Follower", "Фолловер")
	}
	return structuredText(ru, "Leader", "Лидер")
}

func (s *Service) structuredPaymentsView(ru bool) string {
	if s.options.MiniAppURL == "" {
		return structuredText(ru,
			"The pass payment form is currently unavailable. Try /payments later.",
			"Форма оплаты пасса сейчас недоступна. Попробуй /payments позже.")
	}
	return structuredText(
		ru,
		"Send your payment proof to this chat, then open the form to choose your pass, file and amount. "+
			"Submission shares the proof for review; "+
			"only an ambassador or event administrator can confirm payment.",
		"Пришли подтверждение оплаты в этот чат, затем открой форму и выбери пасс, файл и сумму. "+
			"Файл отправляется на проверку; оплату подтверждает амбассадор или администратор эвента.",
	)
}

func (s *Service) structuredProfileView(ctx context.Context, scope db.ProfileScope, ru bool) (string, error) {
	profile, err := s.store.GetProfile(ctx, scope)
	if err != nil {
		return "", err
	}
	name := profile.FullName
	if name == "" {
		name = structuredText(ru, "Not set", "Не указано")
	}
	text := structuredText(ru, "My profile\nName: ", "Мой профиль\nИмя: ") + name
	text += structuredText(ru, "\n/name — Change name", "\n/name — Изменить имя")
	if profile.NameExpectation != nil {
		text += structuredText(ru, "\nA name request is pending.", "\nЕсть незавершённый запрос имени.")
	}
	return text, nil
}
