package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func foodBotFixture(t *testing.T) (*fixture, legacyfood.Service) {
	t.Helper()
	f := setup(t)
	server := httptest.NewServer(
		api.Handler(
			notificationFixtureServices(f.db, appservices.Options{LegacyOrderBotID: 77}),
			f.b.Host.Signer,
			slog.New(slog.DiscardHandler),
		),
	)
	t.Cleanup(server.Close)
	f.b.API.Base = server.URL
	f.b.Host.Base = f.b.API.Base
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at) VALUES('food-bot','2035-01-01Z');
 INSERT INTO core.food_events(event_id,bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after)
 VALUES('food-bot',77,'{"friday":{"lunch":[{"title_ru":"Суп","title_en":"Soup","price":185}]}}',repeat('a',64),
 '{"with_soup":665,"without_soup":555}','{"party":2000,"party_and_classes":2500,"all_classes":2000,"yoga":750,"cacao":1000,"soundhealing":1000}',
 '2035-01-01Z',38,'7 days','1 day','1 hour');
 INSERT INTO core.food_admins(event_id,owner,can_export,can_review,can_assign) VALUES('food-bot','bob',true,true,true)`)
	require.NoError(t, err)
	return f, legacyfood.Service{DB: f.db, BotID: 77, Delivery: syntheticDeliverySettings()}
}

func TestFoodBotReceiptExplicitTargetReplayAndCurrentReviewer(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	order, err := service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{EventID: "food-bot", Name: "toggle_activity", Activity: "open", Key: "party"},
	)
	require.NoError(t, err)
	order, err = service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{
			EventID: order.EventID,
			OrderID: order.ID,
			Version: order.Version,
			Name:    "begin_payment",
			Kind:    legacyfood.Activity,
			Key:     "pay",
		},
	)
	require.NoError(t, err)
	attachment, err := (media.Service{DB: f.db}).Upload(
		t.Context(),
		"alice",
		"receipt.pdf",
		[]byte("%PDF-1.4 food receipt"),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,status) VALUES('food-receipt','alice',8000,$1,'choose')`,
		attachment.ID,
	)
	require.NoError(t, err)
	command := legacyfood.Command{
		EventID: order.EventID,
		OrderID: order.ID,
		Version: order.Version,
		Name:    "submit_proof",
		Kind:    legacyfood.Activity,
		Key:     "food-receipt",
	}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.food_buttons(owner,token,command) VALUES('alice','receipt',$1)`,
		map[string]any{"command": command, "media_id": "food-receipt"},
	)
	require.NoError(t, err)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Receipt target"})
	require.NoError(t, err)
	handle(t, f.b, aliceCallback(8001, old.ID, "food:receipt"))
	handle(t, f.b, aliceCallback(8001, old.ID, "food:receipt"))
	view, err := service.View(t.Context(), "alice", "food-bot", order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, view.Order.ActivityPayment.Status)
	assert.Equal(t, legacyfood.Pending, view.Order.MealPayment.Status)
	assert.EqualValues(t, 1, view.Order.ActivityPayment.Generation)
	require.NoError(t, f.b.DeliverFoodNotifications(t.Context()))
	var token string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT token FROM bot.food_buttons WHERE owner='bob' AND command->'command'->>'name'='accept' LIMIT 1`).
			Scan(&token),
	)
	_, err = f.db.Exec(t.Context(), `UPDATE core.food_admins SET can_review=false WHERE owner='bob'`)
	require.NoError(t, err)
	review := aliceCallback(8002, old.ID, "food:"+token)
	review.Callback.From.ID = 202
	review.Callback.Message.Chat.ID = 202
	handle(t, f.b, review)
	handle(t, f.b, review)
	var notices int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents i
JOIN core.delivery_queue q ON q.bot_id=i.bot_id AND q.owner_kind='bot' AND q.owner_key=i.operation_key AND q.effect_key=i.effect_key
WHERE i.owner='bob' AND i.reference->>'update'='8002' AND i.reference->>'kind'='result'`).Scan(&notices))
	assert.Equal(t, 1, notices, "replayed denial must retain one queued result")
	view, err = service.View(t.Context(), "alice", "food-bot", order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, view.Order.ActivityPayment.Status)
}

func TestFoodBotLegacyBindingOpaqueOwnerAndStaleButtons(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Activities"})
	require.NoError(t, err)
	update := aliceCallback(7801, old.ID, "food|toggle_activity|yoga")
	handle(t, f.b, update)
	handle(t, f.b, update)
	view, err := service.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	assert.True(t, view.Order.Activities["yoga"])
	assert.EqualValues(t, 1, view.Order.Version)
	var token string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT token FROM bot.food_buttons WHERE owner='alice' AND command->'command'->>'activity'='cacao' ORDER BY created_at DESC LIMIT 1`).
			Scan(&token),
	)
	stolen := aliceCallback(7802, old.ID, "food:"+token)
	stolen.Callback.From.ID = 202
	stolen.Callback.Message.Chat.ID = 202
	handle(t, f.b, stolen)
	handle(t, f.b, aliceCallback(7803, old.ID, "food:"+token))
	handle(t, f.b, aliceCallback(7804, old.ID, "food:"+token))
	view, err = service.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	assert.True(t, view.Order.Activities["cacao"])
	assert.EqualValues(t, 2, view.Order.Version)
	var saved legacyfood.Callback
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT binding FROM bot.food_legacy_callbacks WHERE owner='alice'`).Scan(&saved),
	)
	assert.Equal(t, "food-bot", saved.EventID)
}

func TestFoodBotPaymentHintDoesNotConsumeText(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	order, err := service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{
			EventID: "food-bot",
			Name:    "save_meals",
			Key:     "meals",
			Meals: legacyfood.MealSelection{
				"friday": {Lunch: &legacyfood.LunchSelection{Type: "individual-items", Items: json.RawMessage(`[0]`)}},
			},
		},
	)
	require.NoError(t, err)
	command := legacyfood.Command{
		EventID: "food-bot",
		OrderID: order.ID,
		Version: order.Version,
		Name:    "begin_payment",
		Kind:    legacyfood.Meals,
	}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.food_buttons(owner,token,command) VALUES('alice','pay',$1)`,
		map[string]any{"command": command},
	)
	require.NoError(t, err)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Pay"})
	require.NoError(t, err)
	handle(t, f.b, aliceCallback(7901, old.ID, "food:pay"))
	handle(t, f.b, message(7902, 101, "What time does the event start?"))
	require.NotNil(t, f.model.input.Food)
	assert.Equal(t, legacyfood.Meals, f.model.input.Food.Kind)
	var pending int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.food_pending WHERE owner='alice'`).Scan(&pending),
	)
	assert.Equal(t, 1, pending)
	view, err := service.View(t.Context(), "alice", "food-bot", order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Pending, view.Order.MealPayment.Status)
	assert.Empty(t, view.Order.MealPayment.ProofID)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.food_pending SET expires_at=now()-interval '1 second' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	handle(t, f.b, message(7903, 101, "Where is the event?"))
	assert.Nil(t, f.model.input.Food)
}
