package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestScriptFoodPreparationAcrossCurrentEventSwitch(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at,display_order) VALUES('food-second','2035-01-01Z',1);
 INSERT INTO core.food_events(event_id,bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after)
 SELECT 'food-second',bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after FROM core.food_events WHERE event_id='food-bot';
 INSERT INTO core.food_admins(event_id,owner,can_assign) VALUES('food-second','bob',true)`,
	)
	require.NoError(t, err)
	for _, event := range []string{"food-bot", "food-second"} {
		_, err = service.Execute(
			t.Context(),
			"alice",
			legacyfood.Command{EventID: event, Name: "toggle_activity", Activity: "yoga", Key: "seed"},
		)
		require.NoError(t, err)
	}
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				first := scriptFoodRead(ctx, t, callback)
				require.Equal(t, "food-bot", first.Event.ID)
				scriptCall(ctx, t, callback, "food.payment.prepare", `{"kind":"activities"}`)
				_, changeErr := f.db.Exec(ctx, `UPDATE core.pass_events SET display_order=-1 WHERE id='food-second'`)
				require.NoError(t, changeErr)
				second := scriptFoodRead(ctx, t, callback)
				require.Equal(t, "food-second", second.Event.ID)
				scriptCall(ctx, t, callback, "food.payment.prepare", `{"kind":"activities"}`)
				return json.RawMessage(`{"prepared":true}`), nil
			},
		),
	)
	handle(t, f.b, message(1989, identity.AliceTelegramID, "Read my saved history and orders"))
	var pending legacyfood.Command
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT command FROM bot.food_pending WHERE owner='alice'`).Scan(&pending),
	)
	assert.Equal(t, "food-second", pending.EventID)
	view, err := service.View(t.Context(), "alice", "food-second", "")
	require.NoError(t, err)
	assert.Equal(t, view.Order.ID, pending.OrderID)
	assert.Equal(t, view.Order.Version, pending.Version)
	assert.Equal(t, view.Order.ActivityPayment.Generation, pending.Generation)
	var ordinals []int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT array_agg((c->>'food_sequence')::integer ORDER BY (c->>'food_sequence')::integer)
 FROM bot.interactions i,jsonb_array_elements(i.content) r,jsonb_array_elements(r->'calls') c
 WHERE i.owner='alice' AND i.update_id=1989 AND i.kind='script_runs' AND c->'food'->>'name'='begin_payment'`).
			Scan(&ordinals),
	)
	assert.Equal(t, []int{2, 4}, ordinals)
}
