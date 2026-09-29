package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func foodAdmissionSwitch(t *testing.T, f *fixture, name, extra string) {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at,display_order) VALUES('food-next','2035-01-01Z',1);
 INSERT INTO core.food_events SELECT 'food-next',bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after FROM core.food_events WHERE event_id='food-bot';
 INSERT INTO core.food_admins(event_id,owner,can_review,can_export,can_assign) VALUES('food-next','bob',true,true,true);
 CREATE TABLE bot.food_admission_seen(id integer PRIMARY KEY);`,
	)
	require.NoError(t, err)
	// The trigger fires inside the durable script reservation transaction, after
	// preparation and before the HTTP command. A table makes the barrier one-shot.
	_, err = f.db.Exec(
		t.Context(),
		fmt.Sprintf(`CREATE FUNCTION bot.food_admission_switch() RETURNS trigger LANGUAGE plpgsql AS $fn$
 BEGIN
 IF NEW.kind='script_runs' AND EXISTS(SELECT 1 FROM jsonb_path_query(NEW.content,'$[*].calls[*].food') c WHERE c->>'name'='%s' AND c->>'key'<>'') THEN
 INSERT INTO bot.food_admission_seen VALUES(1) ON CONFLICT DO NOTHING;
 IF FOUND THEN
 UPDATE core.pass_events SET display_order=-1 WHERE id='food-next';
 %s
 END IF;
 END IF;
 RETURN NEW;
 END $fn$;
 CREATE TRIGGER food_admission_switch AFTER UPDATE ON bot.interactions FOR EACH ROW EXECUTE FUNCTION bot.food_admission_switch()`, name, extra),
	)
	require.NoError(t, err)
}

func TestScriptFoodAdmittedEventRemainsBound(t *testing.T) {
	t.Parallel()
	f, s := foodBotFixture(t)
	foodAdmissionSwitch(t, f, "toggle_activity", "")
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				first := scriptFoodRead(ctx, t, callback)
				require.Equal(t, "food-bot", first.Event.ID)
				scriptCall(ctx, t, callback, "food.change", `{"name":"toggle_activity","activity":"yoga"}`)
				_, err := callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "food.change",
						Arguments: json.RawMessage(`{"name":"toggle_activity","activity":"cacao"}`),
					},
				)
				require.Error(t, err, "a later call needs a fresh current-event view")
				next := scriptFoodRead(ctx, t, callback)
				require.Equal(t, "food-next", next.Event.ID)
				scriptCall(ctx, t, callback, "food.change", `{"name":"toggle_activity","activity":"cacao"}`)
				return json.RawMessage(`{"changed":true}`), nil
			},
		),
	)
	var saved legacyfood.Command
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT c->'food' FROM bot.interactions i,jsonb_array_elements(i.content) r,jsonb_array_elements(r->'calls') c
 WHERE i.owner='alice' AND i.update_id=1989 AND i.kind='script_runs' AND c->'food'->>'key'='tg-script-1989-0-1'`).
			Scan(&saved),
	)
	assert.Equal(t, "food-bot", saved.EventID)
	assert.Equal(t, "toggle_activity", saved.Name)
	assert.NotEmpty(t, saved.CatalogRevision)
	original, err := s.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	assert.True(t, original.Order.Activities["yoga"])
	assert.False(t, original.Order.Activities["cacao"])
	replay, err := s.Execute(t.Context(), "alice", saved)
	require.NoError(t, err)
	assert.Equal(t, original.Order.Version, replay.Version)
	next, err := s.View(t.Context(), "alice", "food-next", "")
	require.NoError(t, err)
	assert.True(t, next.Order.Activities["cacao"])
	assert.False(t, next.Order.Activities["yoga"])
}

func TestScriptFoodAdmissionPreservesDomainGuards(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, sql string }{
		{"revoked", `UPDATE core.users SET can_book=false WHERE id='alice';`},
		{"archived", `UPDATE core.pass_events SET finishes_at='2000-01-01Z' WHERE id='food-bot';`},
		{"catalog", `UPDATE core.food_events SET activity_prices=jsonb_set(activity_prices,'{yoga}','751') WHERE event_id='food-bot';`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, _ := foodBotFixture(t)
			foodAdmissionSwitch(t, f, "toggle_activity", test.sql)
			runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						scriptFoodRead(ctx, t, callback)
						_, err := callback(
							ctx,
							scriptclient.ToolCall{
								Name:      "food.change",
								Arguments: json.RawMessage(`{"name":"toggle_activity","activity":"yoga"}`),
							},
						)
						require.Error(t, err)
						return json.RawMessage(`{"denied":true}`), nil
					},
				),
			)
			var seen int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.food_admission_seen`).Scan(&seen))
			assert.Equal(t, 1, seen)
			var count int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.food_orders`).Scan(&count))
			assert.Zero(t, count)
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.food_operations`).Scan(&count))
			assert.Zero(t, count)
		})
	}
}

func TestScriptFoodAdmittedReviewUsesOriginalEvent(t *testing.T) {
	t.Parallel()
	f, s, order := foodSubmittedFixture(t)
	foodAdmissionSwitch(t, f, "accept", "")
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			scriptCall(ctx, t, callback, "food.review.read", fmt.Sprintf(`{"order_id":%q}`, order.ID))
			scriptCall(ctx, t, callback, "food.review.decide", `{"kind":"meals","decision":"accept"}`)
			_, err := callback(
				ctx,
				scriptclient.ToolCall{
					Name:      "food.review.decide",
					Arguments: json.RawMessage(`{"kind":"activities","decision":"accept"}`),
				},
			)
			require.Error(t, err, "new review needs current-event evidence")
			return json.RawMessage(`{"accepted":true}`), nil
		},
	)
	f.b.Model = &knowledgeModel{
		plans: []agent.Plan{
			{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
			{View: agent.OrdersView, Text: "Done"},
		},
	}
	handle(t, f.b, message(29001, identity.BobTelegramID, "Accept the observed meals payment"))
	got, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Paid, got.MealPayment.Status)
	assert.Equal(t, legacyfood.Submitted, got.ActivityPayment.Status)
	var saved legacyfood.Command
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT c->'food' FROM bot.interactions i,jsonb_array_elements(i.content) r,jsonb_array_elements(r->'calls') c
 WHERE i.owner='bob' AND i.update_id=29001 AND i.kind='script_runs' AND c->'food'->>'key'='tg-script-29001-0-1'`).
			Scan(&saved),
	)
	assert.Equal(t, order.EventID, saved.EventID)
	assert.Equal(t, order.ID, saved.OrderID)
	assert.Equal(t, order.Version, saved.Version)
	assert.Equal(t, order.MealPayment.Generation, saved.Generation)
	replay, err := s.Execute(t.Context(), "bob", saved)
	require.NoError(t, err)
	assert.Equal(t, got.Version, replay.Version)
}

func TestScriptFoodAdmittedReviewRechecksGrant(t *testing.T) {
	t.Parallel()
	f, s, order := foodSubmittedFixture(t)
	foodAdmissionSwitch(
		t,
		f,
		"accept",
		`UPDATE core.food_admins SET can_review=false WHERE event_id='food-bot' AND owner='bob';`,
	)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			scriptCall(ctx, t, callback, "food.review.read", fmt.Sprintf(`{"order_id":%q}`, order.ID))
			_, err := callback(
				ctx,
				scriptclient.ToolCall{
					Name:      "food.review.decide",
					Arguments: json.RawMessage(`{"kind":"meals","decision":"accept"}`),
				},
			)
			require.Error(t, err)
			return json.RawMessage(`{"denied":true}`), nil
		},
	)
	runFoodReviewHost(t, f, 29002)
	got, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, got.MealPayment.Status)
	var seen int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.food_admission_seen`).Scan(&seen))
	assert.Equal(t, 1, seen)
}
