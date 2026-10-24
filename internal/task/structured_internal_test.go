package task

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredPaymentShowsDepositAndFullDeadlines(t *testing.T) {
	t.Parallel()
	initial := time.Date(2030, time.May, 1, 18, 0, 0, 0, time.FixedZone("EEST", 3*60*60))
	final := initial.AddDate(0, 0, 10)
	pass := db.AccountPass{
		InitialDueAt: initial,
		FinalDueAt:   final,
		DueAt:        initial,
		Balance: payments.Balance{
			PriceMinor:           10000,
			RemainingMinor:       10000,
			DepositRequiredMinor: 2000,
			Currency:             "EUR",
			CurrencyScale:        2,
		},
	}
	text := structuredPaymentDeadlines(pass, "Europe/Helsinki", false)
	require.Contains(t, text, "Remaining to secure deposit: 20.00 EUR")
	require.Contains(t, text, "Deposit due: 2030-05-01 18:00 (Europe/Helsinki)")
	require.Contains(t, text, "Full payment due: 2030-05-11 18:00 (Europe/Helsinki)")
	pass.Balance.ConfirmedMinor = 500
	require.Contains(t, structuredPaymentDeadlines(pass, "Europe/Helsinki", false), "15.00 EUR")
	pass.Balance.DepositSatisfied = true
	text = structuredPaymentDeadlines(pass, "Europe/Helsinki", false)
	require.Contains(t, text, "Deposit confirmed.")
	require.NotContains(t, text, "Deposit due:")
	pass.Balance.ConfirmedMinor = pass.Balance.PriceMinor
	pass.Balance.RemainingMinor = 0
	pass.Balance.FullyPaid = true
	require.Equal(t, "\nПолностью оплачен.", structuredPaymentDeadlines(pass, "Europe/Helsinki", true))
}

func TestStructuredPaginationCallbackIsOwnerBoundAndNeverAnApproval(t *testing.T) {
	t.Parallel()
	after := uuid.New()
	out := structuredOutput(intent{ChatID: 7}, "Page", &after, true, "")
	var payload telegram.SendMessageRequest
	require.NoError(t, json.Unmarshal(out[0].Payload, &payload))
	markup, ok := payload.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	button := markup.InlineKeyboard[0][0]
	require.Equal(t, "Следующая страница", button.Text)
	update := telegram.Update{ID: 1, CallbackQuery: &telegram.CallbackQuery{From: telegram.User{ID: 7},
		Message: &telegram.Message{Chat: telegram.Chat{ID: 7, Type: "private"}}, Data: button.CallbackData}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	request, err := normalize(update, raw)
	require.NoError(t, err)
	require.Equal(t, kindStructured, request.Kind)
	require.Equal(t, "/registrations "+after.String(), request.Text)
	require.Equal(t, uuid.Nil, request.CallbackTaskID)
	update.CallbackQuery.Message.Chat.ID = 8
	_, err = normalize(update, raw)
	require.ErrorIs(t, err, ErrUnsupportedUpdate)
	update.CallbackQuery.Message.Chat.ID = 7
	update.CallbackQuery.Data = "r:not-a-cursor"
	_, err = normalize(update, raw)
	require.ErrorIs(t, err, ErrUnsupportedUpdate)
}

// The native entry point must introduce the same named assistant without hiding
// the service commands behind the conversational introduction.
func TestStructuredMenuIntroducesAssistantAndKeepsCommands(t *testing.T) {
	t.Parallel()
	service := &Service{}
	for _, ru := range []bool{false, true} {
		for _, command := range []string{startCommand, menuCommand} {
			text, next, err := service.structuredView(context.Background(), db.ProfileScope{}, command, ru)
			require.NoError(t, err)
			require.Nil(t, next)
			require.Contains(t, text, "Zouk Non Stop")
			if ru {
				require.Contains(t, text, "Я ЗиНуСя, помощница")
			} else {
				require.Contains(t, text, "I’m Zinusya, your assistant")
			}
			for _, available := range []string{profileCommand, registrationsCommand, registerCommand,
				ordersCommand, massageCommand, avatarCommand, pairsCommand, paymentsCommand, knowledgeCommand} {
				require.Contains(t, text, available+" — ")
			}
		}
	}
}
