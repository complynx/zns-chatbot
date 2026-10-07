package integration_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestOrderRenderButtonsKeepDurableOwnerAndCurrentPermission(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	for index := range 2 {
		_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
			EventID: "sandbox-festival", Name: "create", Key: "render-button-" + strconv.Itoa(index),
			Origin: "manual", Choice: orderChoice("preparty"),
		})
		require.NoError(t, err)
	}
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	click := orderClick(t, f, 101, 97001, "Delete order")
	restartOrderBot(f)

	foreign := click
	callback := *click.Callback
	foreign.Callback = &callback
	foreign.Callback.From.ID = 303
	foreign.Callback.Message.Chat.ID = 303
	handleVisible(t, f.b, foreign)
	require.Contains(t, orderChatText(t, f, 303), "Кнопка недоступна")

	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	click.ID = 97002
	handleVisible(t, f.b, click)
	require.Contains(t, orderChatText(t, f, 101), "forbidden")
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='alice'`)
	require.NoError(t, err)
	list, err := (orders.Service{DB: f.db}).List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 2, "foreign and revoked-permission callbacks must not change orders")

	click.ID = 97003
	handleVisible(t, f.b, click)
	handleVisible(t, f.b, click)
	list, err = (orders.Service{DB: f.db}).List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1, "a recreated reader resolves the durable callback without duplicate effects")
}

func TestOrderRenderButtonFailureDoesNotPublishOrderCard(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Key: "render-button-failure",
		Origin: "manual", Choice: orderChoice("preparty"),
	})
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `CREATE FUNCTION bot.reject_order_card_button() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.command->>'order_id' IS NOT NULL THEN RAISE EXCEPTION 'synthetic callback persistence failure'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER reject_order_card_button BEFORE INSERT ON bot.order_buttons
 FOR EACH ROW EXECUTE FUNCTION bot.reject_order_card_button()`)
	require.NoError(t, err)
	require.Error(t, f.b.RenderOrders(t.Context(), "alice", 101))
	pending := botDeliveryCandidates(t, f.b)
	require.NotEmpty(t, pending)
	for _, intent := range pending {
		require.Error(t, f.b.DeliverBotIntent(t.Context(), intent.Reference))
	}
	for _, msg := range chatMessages(t, f, 101) {
		require.NotContains(t, msg.Text, "Status: Unpaid", "a card cannot expose uncommitted callbacks")
	}
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_order_card_button ON bot.order_buttons`)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	require.Contains(t, orderChatText(t, f, 101), "Delete order")
}

func logOrderPagingDeliveryState(t *testing.T, f *fixture) {
	t.Helper()
	var state []byte
	err := f.db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'page',(SELECT page FROM bot.order_pages WHERE owner='alice' AND scope='orders' AND event_id='sandbox-festival'),
 'orders',(SELECT count(*) FROM core.orders WHERE owner='alice' AND state<>'deleted'),
 'visible',(SELECT count(*) FROM bot.order_cards WHERE owner='alice' AND card_key LIKE 'page-%' AND visible),
 'inbox',(SELECT count(*) FROM bot.telegram_inbox),
 'cursors',(SELECT jsonb_object_agg(name,value) FROM bot.cursors),
 'callbacks',(SELECT count(*) FROM bot.order_buttons WHERE owner='alice'),
 'queue',(SELECT jsonb_agg(to_jsonb(head)) FROM (
 SELECT q.owner_kind,q.state,q.lane_sequence,q.not_before,i.state AS intent_state,i.reason,i.phase,
 GREATEST(q.not_before,COALESCE(b.not_before,'-infinity'::timestamptz),COALESCE(c.not_before,'-infinity'::timestamptz)) AS due,
 clock_timestamp() AS observed
 FROM core.delivery_queue q
 LEFT JOIN bot.delivery_intents i ON i.bot_id=q.bot_id AND i.operation_key=q.owner_key AND i.effect_key=q.effect_key
 LEFT JOIN core.delivery_pacing b ON b.bot_id=q.bot_id AND b.chat=''
 LEFT JOIN core.delivery_pacing c ON c.bot_id=q.bot_id AND c.chat=q.chat
 WHERE q.bot_id=$1 AND q.state IN ('pending','sending','unknown','parked','paused')
 ORDER BY q.lane_sequence LIMIT 5) head))`, f.b.Delivery.BotID).Scan(&state)
	t.Logf("order paging delivery state=%s error=%v", state, err)
}
