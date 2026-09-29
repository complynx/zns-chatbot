package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestLanguageOperationReplayPreservesNewerChoice(t *testing.T) {
	t.Parallel()
	f := setup(t)
	value, err := f.b.API.SetLanguageWithOperation(t.Context(), "alice", "en", false, "first")
	require.NoError(t, err)
	assert.Equal(t, "en", value.Language)
	value, err = f.b.API.SetLanguageWithOperation(t.Context(), "alice", "ru", false, "second")
	require.NoError(t, err)
	assert.Equal(t, "ru", value.Language)
	value, err = f.b.API.SetLanguageWithOperation(t.Context(), "alice", "en", false, "first")
	require.NoError(t, err)
	assert.Equal(t, "ru", value.Language)
	_, err = f.b.API.SetLanguageWithOperation(t.Context(), "alice", "ru", false, "first")
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, http.StatusConflict, problem.Status)
	assert.Equal(t, "operation_key_reused", problem.Code)
	value, err = f.b.API.SetLanguageWithOperation(t.Context(), "bob", "en", false, "first")
	require.NoError(t, err)
	assert.Equal(t, "en", value.Language)
	_, err = f.b.API.SetLanguageWithOperation(
		t.Context(),
		"alice",
		"en",
		false,
		core.LanguageOperationKey(strings.Repeat("k", 129)),
	)
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, http.StatusBadRequest, problem.Status)
}

func TestLanguageInterruptedDeliveryReplayRefreshesCurrentCards(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handle(t, f.b, message(710, 101, "/language ru"))
	handle(t, f.b, message(711, 101, "/orders"))
	post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
	interrupted := message(712, 101, "/language en")
	require.Error(t, f.b.Handle(t.Context(), interrupted))
	value, err := f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "en", value.Language)
	var records int
	err = f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='alice' AND update_id=712 AND kind='language'`).
		Scan(&records)
	require.NoError(t, err)
	assert.Zero(t, records)
	var languageID int64
	err = f.db.QueryRow(t.Context(), `SELECT message_id FROM bot.order_cards WHERE owner='alice' AND card_key='language'`).
		Scan(&languageID)
	require.NoError(t, err)
	handle(t, f.b, aliceCallback(713, languageID, "language:ru"))
	handle(t, f.b, interrupted)
	value, err = f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "ru", value.Language)
	handle(t, f.b, aliceCallback(714, languageID, "language:en"))
	handle(t, f.b, aliceCallback(713, languageID, "language:ru"))
	value, err = f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "en", value.Language)
	current, err := i18n.Translate("en", i18n.LanguageCurrent, map[string]string{"language": "en"})
	require.NoError(t, err)
	found := false
	for _, card := range chatMessages(t, f, 101) {
		if card.ID == languageID {
			found = true
			assert.Contains(t, card.Text, current)
		}
	}
	assert.True(t, found)
	err = f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).Scan(&records)
	require.NoError(t, err)
	assert.Equal(t, 4, records)
}

func TestPreferencesInitializePreservesRequestedLocaleAndExplicitChoice(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='' WHERE id='alice'`)
	require.NoError(t, err)
	value, err := f.b.API.SetLanguage(t.Context(), "alice", "ua", true)
	require.NoError(t, err)
	assert.Equal(t, "uk", value.Language)
	assert.Equal(t, i18n.Russian, i18n.NormalizeLocale(value.Language))
	value, err = f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	assert.Equal(t, "en", value.Language)
	value, err = f.b.API.SetLanguage(t.Context(), "alice", "be-BY", true)
	require.NoError(t, err)
	assert.Equal(t, "en", value.Language)
	_, err = f.b.API.SetLanguage(t.Context(), "alice", "pl", false)
	require.Error(t, err)
	other, err := f.b.API.Preferences(t.Context(), "bob")
	require.NoError(t, err)
	assert.Equal(t, "ru", other.Language)
	_, err = f.b.API.SetLanguage(t.Context(), "alice", strings.Repeat("x", 65), true)
	require.Error(t, err)
}

func TestTelegramLanguageInitializesOnceAndDoesNotOpenOrders(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='' WHERE id='alice'`)
	require.NoError(t, err)
	update := message(950, 101, "/language")
	update.Message.From.LanguageCode = "ua"
	handle(t, f.b, update)
	pref, err := f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "uk", pref.Language)
	assert.Len(t, chatMessages(t, f, 101), 1)
	handle(t, f.b, message(951, 101, "/language en"))
	update.ID = 952
	update.Message.From.LanguageCode = "ru"
	handle(t, f.b, update)
	pref, err = f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "en", pref.Language)
	assert.Len(t, chatMessages(t, f, 101), 1)
}

func paymentMessage(t *testing.T, f *fixture) telegram.Message {
	t.Helper()
	for _, message := range chatMessages(t, f, 101) {
		if strings.HasPrefix(message.Text, "Payment for order") || strings.HasPrefix(message.Text, "Оплата заказа") {
			return message
		}
	}
	t.Fatal("payment card not found")
	return telegram.Message{}
}

func TestLanguageSwitchRefreshesPaymentCardAndRevocationRemovesInstructions(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "localized",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	handle(t, f.b, message(700, 101, "/language en"))
	handle(t, f.b, message(701, 101, "/orders"))
	label, err := i18n.Translate("en", i18n.PaymentMethods, nil)
	require.NoError(t, err)
	handle(t, f.b, orderClick(t, f, 101, 702, label))
	english := paymentMessage(t, f)
	assert.Contains(t, english.Text, "TEST PAYMENT DETAILS")
	assert.Contains(t, english.Text, "1,050.00")
	require.NotEmpty(t, english.Markup.Rows)
	assert.Equal(t, "tg://user?id=202", english.Markup.Rows[0][0].URL)
	handle(t, f.b, message(703, 101, "/language ru"))
	handle(t, f.b, message(700, 101, "/language en"))
	russian := paymentMessage(t, f)
	assert.Equal(t, english.ID, russian.ID)
	assert.Contains(t, russian.Text, "ТЕСТОВЫЕ РЕКВИЗИТЫ")
	assert.Contains(t, russian.Text, "35,00")
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	for _, current := range chatMessages(t, f, 101) {
		if current.ID == russian.ID {
			assert.NotContains(t, current.Text, "ТЕСТОВЫЕ РЕКВИЗИТЫ")
			assert.Contains(t, current.Text, "payment_context_unavailable")
			assert.Empty(t, current.Markup.Rows)
		}
	}
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order.Version, current.Version)
	assert.Equal(t, "unpaid", current.State)
}
