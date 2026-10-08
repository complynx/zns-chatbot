package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestMissingDefaultOrderEventCompletesWithVisibleRefusal(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.order_events(id,deadline,menu,extras)
 SELECT 'event_one',deadline,menu,extras FROM core.order_events WHERE id='sandbox-festival'`)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.order_admins WHERE event_id='sandbox-festival'`)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.order_events WHERE id='sandbox-festival'`)
	require.NoError(t, err)
	_, err = f.b.API.OrderEvent(t.Context(), "alice", "sandbox-festival")
	requireCode(t, err, "event_not_found")
	_, err = f.b.API.OrderEvent(t.Context(), "alice", "event_one")
	require.NoError(t, err)

	require.NoError(t, f.b.Handle(t.Context(), message(21, 101, "/orders")),
		"a missing default event must not keep this chat's inbox head retrying")
	pumpBotDeliveries(t, f.b)
	visible := chatMessages(t, f, 101)
	require.Len(t, visible, 1, "the requester needs one durable refusal")
	text, err := i18n.Translate("ru", i18n.OrderEventUnavailable, nil)
	require.NoError(t, err)
	assert.Equal(t, text, visible[0].Text)
	assert.Empty(t, visible[0].Markup.Rows, "missing event must not offer an order mutation")
	handleVisible(t, f.b, message(21, 101, "/orders"))
	assert.Len(t, chatMessages(t, f, 101), 1, "replayed input must not duplicate the refusal")
	handleVisible(t, f.b, message(22, 101, "/language"))
	language := orderClick(t, f, 101, 23, "en")
	choose, err := i18n.Translate("ru", i18n.LanguageChoose, map[string]string{"language": "ru"})
	require.NoError(t, err)
	current, err := i18n.Translate("ru", i18n.LanguageCurrent, map[string]string{"language": "ru"})
	require.NoError(t, err)
	assert.Equal(t, choose+"\n"+current, language.Callback.Message.Text)
	handleVisible(t, f.b, language)
	preference, err := f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "en", preference.Language)
	saved, err := i18n.Translate("en", i18n.LanguageSaved, map[string]string{"language": "en"})
	require.NoError(t, err)
	current, err = i18n.Translate("en", i18n.LanguageCurrent, map[string]string{"language": "en"})
	require.NoError(t, err)
	russian := orderClick(t, f, 101, 24, "ru")
	assert.Equal(t, saved+"\n"+current, russian.Callback.Message.Text,
		"the next command must deliver a usable card and persist its callback")
	handleVisible(t, f.b, russian)

	f.b.OrderEventID = "event_one"
	handleVisible(t, f.b, message(25, 101, "/orders"))
	orderClick(t, f, 101, 26, "Новый заказ")
}

func TestMissingDefaultOrderEventDoesNotHideDatabaseFailure(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `ALTER TABLE core.order_events RENAME TO unavailable_order_events`)
	require.NoError(t, err)
	require.Error(t, f.b.Handle(t.Context(), message(21, 101, "/orders")),
		"a database failure must remain retryable, not become a missing-event refusal")
	assert.Empty(t, chatMessages(t, f, 101))
}
