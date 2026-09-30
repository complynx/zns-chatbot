package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type unavailableWorkflowModel struct{ calls int }

func (m *unavailableWorkflowModel) Plan(context.Context, agent.Input) (agent.Plan, error) {
	m.calls++
	return agent.Plan{}, errors.New("synthetic model failure")
}

func TestWorkflowUnavailableNoticeLocaleAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Delivery.Fallback = time.Second
	model := &unavailableWorkflowModel{}
	f.b.Model = model
	handleVisible(t, f.b, message(9020, 101, "/language en"))
	handleVisible(t, f.b, message(9021, 101, "/start"))
	english, err := i18n.Translate("en", i18n.AgentUnavailable, nil)
	require.NoError(t, err)
	russian, err := i18n.Translate("ru", i18n.AgentUnavailable, nil)
	require.NoError(t, err)
	post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
	request := message(9022, 101, "help")
	require.NoError(t, f.b.Handle(t.Context(), request))
	failed := assertBotRateLimited(t, f)
	var saved, identity string
	var native bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content#>>'{}',native_markdown FROM bot.interactions
 WHERE owner='alice' AND update_id=9022 AND kind='reply'`).Scan(&saved, &native))
	assert.Equal(t, english, saved)
	assert.False(t, native, "catalog fallback is a system notice, not model prose")
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE(payload->>'system_notice','') FROM interaction.saved_turns WHERE owner='alice' AND update_id=9022`).
			Scan(&identity),
	)
	assert.Equal(t, string(i18n.AgentUnavailable), identity)
	handle(t, f.b, message(9023, 101, "/language ru"))
	waitBotRetryDeadline(t, f, failed)
	pumpBotDeliveries(t, f.b)
	assert.Contains(t, workflowCard(t, f).Text, russian)
	before := chatMessages(t, f, 101)
	handleVisible(t, f.b, request)
	assert.Equal(t, before, chatMessages(t, f, 101), "replay must not create another message")
	assert.Equal(t, 1, model.calls, "transport replay must use the persisted fallback identity")
	assert.Contains(t, workflowCard(t, f).Text, russian)
	handleVisible(t, f.b, message(9024, 101, "/language en"))
	assert.Contains(t, workflowCard(t, f).Text, english)
}

func assertBotRateLimited(t *testing.T, f *fixture) botdelivery.Intent {
	t.Helper()
	pumpBotDeliveries(t, f.b)
	ref := delivery.Reference{Owner: delivery.Bot}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner='alice' AND state='pending' AND reason='telegram_rate_limit'`).Scan(&ref.Key, &ref.Effect))
	failed, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, failed.Attempt)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
	beforeDeadline, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, readErr)
	require.Equal(t, failed.Attempt, beforeDeadline.Attempt, "provider retry deadline prevents another wire attempt")
	return failed
}

func waitBotRetryDeadline(t *testing.T, f *fixture, failed botdelivery.Intent) {
	t.Helper()
	require.Eventually(t, func() bool {
		var ready bool
		err := f.db.QueryRow(t.Context(), `SELECT clock_timestamp()>=$1`, failed.NotBefore).Scan(&ready)
		return err == nil && ready
	}, 5*time.Second, 10*time.Millisecond)
}

func workflowCard(t *testing.T, f *fixture) telegram.Message {
	t.Helper()
	var id int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT message_id FROM bot.messages WHERE owner='alice'`).Scan(&id))
	for _, card := range chatMessages(t, f, 101) {
		if card.ID == id {
			return card
		}
	}
	t.Fatal("workflow card missing")
	return telegram.Message{}
}

func TestWorkflowLanguageRefreshAndActionNotices(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(9000, 101, "/language en"))
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.messages WHERE owner='alice'`).Scan(&count))
	assert.Zero(t, count, "language settings must not open a workflow card")
	handleVisible(t, f.b, message(9001, 101, "/start"))
	card := workflowCard(t, f)
	assert.Contains(t, card.Text, "Choose a service")
	assert.Contains(t, card.Text, "Status: Not selected")
	handleVisible(t, f.b, aliceCallback(9002, card.ID, "select:shuttle-1:0"))
	card = workflowCard(t, f)
	assert.Contains(t, card.Text, "State updated: Draft")
	assert.Equal(t, "Confirm", card.Markup.Rows[0][0].Text)
	handleVisible(t, f.b, message(9003, 101, "/language ru"))
	translated := workflowCard(t, f)
	assert.Equal(t, card.ID, translated.ID)
	assert.Contains(t, translated.Text, "Состояние обновлено: Черновик")
	assert.Contains(t, translated.Text, "Статус: Черновик")
	assert.Equal(t, "Подтвердить", translated.Markup.Rows[0][0].Text)
	handleVisible(t, f.b, aliceCallback(9004, card.ID, "confirm::0"))
	assert.Contains(t, workflowCard(t, f).Text, "Действие не выполнено:")
	handleVisible(t, f.b, message(9005, 101, "/language en"))
	assert.Contains(t, workflowCard(t, f).Text, "Action failed:")
	handleVisible(t, f.b, aliceCallback(9006, card.ID, "broken"))
	assert.Contains(t, workflowCard(t, f).Text, "Invalid button.")
	handleVisible(t, f.b, message(9007, 101, "/language ru"))
	assert.Contains(t, workflowCard(t, f).Text, "Некорректная кнопка.")
}

func TestWorkflowLanguagePreservesAgentReply(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.model.plan = agent.Plan{View: "workflow", Text: "Ответ remains unchanged"}
	handleVisible(t, f.b, message(9010, 101, "question"))
	card := workflowCard(t, f)
	handleVisible(t, f.b, message(9011, 101, "/language en"))
	translated := workflowCard(t, f)
	assert.Equal(t, card.ID, translated.ID)
	assert.Contains(t, translated.Text, "Ответ remains unchanged")
	assert.Contains(t, translated.Text, "Status: Not selected")
}
