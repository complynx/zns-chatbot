package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const foodAgentMediaID = "agent-food-receipt"

func foodAgentReceiptFixture(t *testing.T) (*fixture, legacyfood.Service, legacyfood.Order) {
	t.Helper()
	f, service := foodBotFixture(t)
	order, err := service.Execute(t.Context(), "alice", legacyfood.Command{
		EventID: "food-bot",
		Name:    "save_meals",
		Key:     "agent-meals",
		Meals: legacyfood.MealSelection{
			"friday": {Lunch: &legacyfood.LunchSelection{Type: "individual-items", Items: json.RawMessage(`[0]`)}},
		},
	})
	require.NoError(t, err)
	order, err = service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{
			EventID:  order.EventID,
			OrderID:  order.ID,
			Version:  order.Version,
			Name:     "toggle_activity",
			Activity: "open",
			Key:      "agent-party",
		},
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
			Key:     "agent-pay",
		},
	)
	require.NoError(t, err)
	attachment, err := (media.Service{DB: f.db}).Upload(
		t.Context(),
		"alice",
		"receipt.pdf",
		[]byte("%PDF-1.4 agent receipt"),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,status) VALUES($1,'alice',8700,$2,'choose')`,
		foodAgentMediaID,
		attachment.ID,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, foodAgentMediaID))
	return f, service, order
}

type foodExportSecondFailure struct {
	documents  atomic.Int64
	rejectedAt atomic.Int64
	retriedAt  atomic.Int64
	uncertain  bool
}

func (f *foodExportSecondFailure) RoundTrip(request *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(request.URL.Path, "/sendDocument") {
		return http.DefaultTransport.RoundTrip(request)
	}
	attempt := f.documents.Add(1)
	if attempt == 3 {
		f.retriedAt.Store(time.Now().UnixNano())
	}
	if attempt == 2 {
		status := http.StatusTooManyRequests
		body := "{\"ok\":false,\"error_code\":429,\"description\":\"synthetic cooldown\",\"parameters\":{\"retry_after\":5}}"
		if f.uncertain {
			status = http.StatusServiceUnavailable
			body = "{\"ok\":false,\"description\":\"synthetic temporary failure\"}"
		}
		f.rejectedAt.Store(time.Now().UnixNano())
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestFoodCSVPartialRetryAndRevokedGrant(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"retry", "revoked", "uncertain"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testFoodCSVDeliveryBoundary(t, scenario)
		})
	}
}

func testFoodCSVDeliveryBoundary(t *testing.T, scenario string) {
	t.Helper()
	f, _, _ := foodAgentReceiptFixture(t)
	transport := &foodExportSecondFailure{uncertain: scenario == "uncertain"}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	exported, err := f.b.API.ExportFood(t.Context(), "bob", "food-bot")
	require.NoError(t, err)
	update := message(8740, 202, "/exportfoodorders")
	require.NoError(t, f.b.Handle(t.Context(), update))
	require.Zero(t, transport.documents.Load(), "admission does not send documents")
	first := foodCSVReference("food_orders_export")
	second := foodCSVReference("food_summary_export")
	waitExportBoundaryCandidate(t, f, first, time.Second)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), first))
	firstSent := assertFoodCSVReceipt(t, f, first, "food_orders_food-bot.csv", exported.Orders)
	waitExportBoundaryCandidate(t, f, second, time.Second)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), second),
		"provider outcome is persisted by the delivery owner")
	require.EqualValues(t, 2, transport.documents.Load())
	require.Equal(t, 1, foodCSVProjectionCount(t, f))
	pending, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, second, false)
	require.NoError(t, err)
	require.Zero(t, pending.MessageID)
	require.False(t, pending.ContinuationDone)
	require.EqualValues(t, 1, pending.Attempt)
	restarted := *f.b
	f.b = &restarted
	if scenario == "uncertain" {
		require.Equal(t, delivery.Uncertain, pending.State)
		// Revocation cannot turn a possibly accepted remote send into a pre-send cancellation.
		revokeFoodCSVGrant(t, f)
		assertFoodCSVReplay(t, f, update, true)
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), second))
		after, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, second, false)
		require.NoError(t, readErr)
		require.Equal(t, delivery.Uncertain, after.State)
		require.Equal(t, pending, after, "uncertain outcome and receipt must remain immutable")
		require.Zero(t, after.MessageID)
		require.EqualValues(t, 2, transport.documents.Load())
		require.Equal(t, 1, foodCSVProjectionCount(t, f))
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), first))
		require.Equal(t, firstSent, assertFoodCSVReceipt(t, f, first, "food_orders_food-bot.csv", exported.Orders))
		return
	}
	require.Equal(t, delivery.Deferred, pending.State)
	require.True(t, pending.NotBefore.After(time.Now()))
	require.Positive(t, transport.rejectedAt.Load())
	cooldownEnd := time.Unix(0, transport.rejectedAt.Load()).Add(5 * time.Second)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), second))
	require.EqualValues(t, 2, transport.documents.Load(), "cooldown prevents an early wire call")
	if scenario == "revoked" {
		revokeFoodCSVGrant(t, f)
	}
	waitExportBoundaryCandidate(t, f, second, 10*time.Second)
	require.False(t, time.Now().Before(cooldownEnd), "eligibility must honor all five retry_after seconds")
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), second))
	after, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, second, false)
	require.NoError(t, err)
	require.Equal(t, pending.Reference, after.Reference)
	if scenario == "revoked" {
		require.Equal(t, delivery.Cancelled, after.State)
		require.Zero(t, after.MessageID)
		require.EqualValues(t, 2, transport.documents.Load())
		require.Equal(t, 1, foodCSVProjectionCount(t, f))
	} else {
		assertFoodCSVReceipt(t, f, second, "meal_summary_food-bot.csv", exported.Summary)
		require.EqualValues(t, 3, transport.documents.Load())
		require.Positive(t, transport.retriedAt.Load())
		require.False(t, time.Unix(0, transport.retriedAt.Load()).Before(cooldownEnd),
			"the actual retry wire call must honor all five retry_after seconds")
		require.Equal(t, 2, foodCSVProjectionCount(t, f))
	}
	calls := transport.documents.Load()
	assertFoodCSVReplay(t, f, update, scenario == "revoked")
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), first))
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), second))
	require.Equal(t, calls, transport.documents.Load(), "replay must not resend either document")
	replayed, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, second, false)
	require.NoError(t, readErr)
	require.Equal(t, after, replayed, "replay must preserve the final delivery state and receipt")
	require.Equal(t, firstSent, assertFoodCSVReceipt(t, f, first, "food_orders_food-bot.csv", exported.Orders))
}

// Manual export admission rechecks current capability, even for a known update.
// The capability refusal is an ordinary Bot error, not the API's typed forbidden response.
func assertFoodCSVReplay(t *testing.T, f *fixture, update telegram.Update, denied bool) {
	t.Helper()
	err := f.b.Handle(t.Context(), update)
	if denied {
		require.EqualError(t, err, "food export unavailable")
		return
	}
	require.NoError(t, err)
}

func foodCSVReference(family string) delivery.Reference {
	operation, effect := botdelivery.ResultOperation("bob", 8740, "document:"+family+":food-bot:")
	return delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
}

func foodCSVProjectionCount(t *testing.T, f *fixture) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		"SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=8740 AND kind IN ('food_orders_export','food_summary_export')",
	).
		Scan(&count))
	return count
}

func revokeFoodCSVGrant(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), "UPDATE core.food_admins SET can_export=false WHERE owner='bob'")
	require.NoError(t, err)
	_, err = f.b.API.ExportFood(t.Context(), "bob", "food-bot")
	requireCode(t, err, "forbidden")
}

func assertFoodCSVReceipt(
	t *testing.T,
	f *fixture,
	ref delivery.Reference,
	filename string,
	expected []byte,
) botdelivery.Intent {
	t.Helper()
	intent, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, intent.State)
	require.True(t, intent.ContinuationDone)
	require.NotNil(t, intent.Receipt.Document)
	require.Equal(t, filename, intent.Receipt.Document.Filename)
	digest := sha256.Sum256(expected)
	require.Equal(t, hex.EncodeToString(digest[:]), intent.Receipt.Document.SHA256)
	require.Equal(t, len(expected), intent.Receipt.Document.Bytes)
	var projectedID int64
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		"SELECT (content->>'message_id')::bigint FROM bot.interactions WHERE owner='bob' AND update_id=8740 AND kind=$1",
		intent.Reference.Family,
	).Scan(&projectedID))
	require.Equal(t, intent.MessageID, projectedID)
	found := 0
	for _, sent := range chatMessages(t, f, 202) {
		if sent.Document == nil || sent.Document.Filename != filename {
			continue
		}
		found++
		require.Equal(t, intent.MessageID, sent.ID)
		body, downloadErr := f.b.TG.Download(t.Context(), *sent.Document)
		require.NoError(t, downloadErr)
		require.Equal(t, expected, body)
	}
	require.Equal(t, 1, found, "exactly one accepted document must match its durable receipt")
	return intent
}
func TestFoodAgentExplicitDisplayedTarget(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, text, kind, mutate string
		submitted                bool
	}{
		{name: "english", text: "Use the activities receipt target for ", submitted: true},
		{name: "russian", text: "Это чек за активности ", kind: legacyfood.Activity, submitted: true},
		{name: "meals", text: "Use the meals receipt target for ", kind: legacyfood.Meals, submitted: true},
		{name: "model_only", text: "What time is check-in? ", kind: legacyfood.Activity},
		{name: "ambiguous", text: "Is this for meals or activities? ", kind: legacyfood.Activity},
		{name: "wrong_kind", text: "Use the activities receipt target for ", kind: legacyfood.Meals},
		{name: "stale", text: "Use the activities receipt target for ", mutate: `UPDATE core.food_orders SET version=version+1 WHERE owner='alice'`},
		{name: "generation", text: "Use the activities receipt target for ", mutate: `UPDATE core.food_payments SET generation=generation+1 WHERE kind='activities'`},
		{name: "receiver_revoked", text: "Use the activities receipt target for ", mutate: `UPDATE core.food_admins SET can_assign=false,can_review=false WHERE owner='bob'`},
		{name: "hidden", text: "Use the activities receipt target for ", mutate: `UPDATE bot.media_intake SET rendered=NULL WHERE owner='alice'`},
		{name: "expired", text: "Use the activities receipt target for ", mutate: `UPDATE bot.media_intake SET expires_at=now()-interval '1 second' WHERE owner='alice'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, service, order := foodAgentReceiptFixture(t)
			if test.mutate != "" {
				_, err := f.db.Exec(t.Context(), test.mutate)
				require.NoError(t, err)
			}
			f.model.plan = agent.Plan{
				View: agent.MediaView,
				Text: "Receipt selection",
				MediaAction: &agent.MediaProposal{
					MediaID:  foodAgentMediaID,
					Intent:   "receipt",
					OrderID:  order.ID,
					FoodKind: test.kind,
				},
			}
			update := message(8701, 101, test.text+order.ID)
			handle(t, f.b, update)
			handle(t, f.b, update)
			current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			payment, other := current.ActivityPayment, current.MealPayment
			if test.kind == legacyfood.Meals {
				payment, other = current.MealPayment, current.ActivityPayment
			}
			assert.Equal(t, legacyfood.Pending, other.Status)
			if !test.submitted {
				assert.Equal(t, legacyfood.Pending, payment.Status)
				return
			}
			require.Equal(t, legacyfood.Submitted, payment.Status)
			assert.EqualValues(t, 1, payment.Generation)
			var bound agent.FoodReceiptTarget
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT payload->'media_resolved_food' FROM interaction.saved_turns WHERE owner='alice' AND update_id=8701`).
					Scan(&bound),
			)
			assert.Equal(t, order.EventID, bound.EventID)
			assert.Equal(t, order.ID, bound.OrderID)
			assert.Equal(t, order.Version, bound.Version)
			assert.Equal(t, payment.Kind, bound.Kind)
			assert.Zero(t, bound.Generation)
			var origin string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT last_origin FROM bot.media_intake WHERE id=$1`, foodAgentMediaID).
					Scan(&origin),
			)
			assert.Equal(t, "agent", origin)
		})
	}
}

func TestFoodAgentContextWithoutModernOrderEvent(t *testing.T) {
	t.Parallel()
	f, service, order := foodAgentReceiptFixture(t)
	f.b.OrderEventID = order.EventID
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_events WHERE id=$1`, order.EventID).Scan(&count),
	)
	require.Zero(t, count)
	f.model.plan = agent.Plan{
		View: agent.MediaView,
		Text: "Receipt selection",
		MediaAction: &agent.MediaProposal{
			MediaID:  foodAgentMediaID,
			Intent:   "receipt",
			OrderID:  order.ID,
			FoodKind: legacyfood.Activity,
		},
	}
	handle(t, f.b, message(8701, 101, "Use the activities receipt target"))
	current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, current.ActivityPayment.Status)
}

func foodAgentButton(t *testing.T, f *fixture, token string, command legacyfood.Command) int64 {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO bot.food_buttons(owner,token,command) VALUES('alice',$1,$2)`,
		token,
		map[string]any{"command": command},
	)
	require.NoError(t, err)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Food card"})
	require.NoError(t, err)
	return old.ID
}

func TestFoodReceiptPreparationAfterMenuEdit(t *testing.T) {
	t.Parallel()
	f, service, order := foodAgentReceiptFixture(t)
	oldPay := legacyfood.Command{
		EventID: order.EventID,
		OrderID: order.ID,
		Version: order.Version,
		Name:    "begin_payment",
		Kind:    legacyfood.Meals,
	}
	messageID := foodAgentButton(t, f, "old-meal-pay", oldPay)
	order, err := service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{
			EventID: order.EventID,
			OrderID: order.ID,
			Version: order.Version,
			Name:    "save_meals",
			Meals:   order.Meals,
			Key:     "menu-resave",
		},
	)
	require.NoError(t, err)
	require.Empty(t, order.PaymentAdmin)
	handle(t, f.b, aliceCallback(8710, messageID, "food:old-meal-pay"))
	var refreshedVersion int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT max((command->'command'->>'version')::bigint) FROM bot.food_buttons WHERE command->'command'->>'name'='begin_payment' AND command->'command'->>'order_id'=$1`, order.ID).
			Scan(&refreshedVersion),
	)
	assert.Equal(t, order.Version, refreshedVersion)
	current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Empty(t, current.PaymentAdmin, "stale button must not mutate current order")
	require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, foodAgentMediaID))
	var hint agent.MediaHint
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT rendered FROM bot.media_intake WHERE id=$1`, foodAgentMediaID).Scan(&hint),
	)
	found := false
	for _, choice := range hint.Choices {
		if choice.Action == "food_prepare_meals" {
			found = true
			assert.Nil(t, choice.FoodTarget)
		}
	}
	require.True(t, found, "missing receiver must expose an explicit preparation button")
	var token string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT token FROM bot.food_buttons WHERE command->>'media_id'=$1 AND command->'command'->>'kind'='meals' AND command->'command'->>'name'='begin_payment' ORDER BY created_at DESC LIMIT 1`, foodAgentMediaID).
			Scan(&token),
	)
	handle(t, f.b, aliceCallback(8711, messageID, "food:"+token))
	current, err = service.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, current.PaymentAdmin)
	assert.Equal(t, legacyfood.Pending, current.MealPayment.Status, "preparation never submits the attachment")
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT rendered FROM bot.media_intake WHERE id=$1`, foodAgentMediaID).Scan(&hint),
	)
	found = false
	for _, choice := range hint.Choices {
		if choice.Action == "food_meals" {
			found = true
			require.NotNil(t, choice.FoodTarget)
			assert.Equal(t, current.Version, choice.FoodTarget.Version)
		}
	}
	require.True(t, found)
	f.model.plan = agent.Plan{
		View: agent.MediaView,
		Text: "Receipt selection",
		MediaAction: &agent.MediaProposal{
			MediaID:  foodAgentMediaID,
			Intent:   "receipt",
			OrderID:  order.ID,
			FoodKind: legacyfood.Meals,
		},
	}
	handle(t, f.b, message(8712, 101, "Use the meals receipt target"))
	current, err = service.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, current.MealPayment.Status)
	assert.Equal(t, legacyfood.Pending, current.ActivityPayment.Status)
}

func TestFoodCSVExportsRemainIndependentOfModernXLSX(t *testing.T) {
	t.Parallel()
	f, _, _ := foodAgentReceiptFixture(t)
	expected, err := f.b.API.ExportFood(t.Context(), "bob", "food-bot")
	require.NoError(t, err)
	visibleFiles := func() (map[string][]byte, int) {
		files := map[string][]byte{}
		count := 0
		for _, message := range chatMessages(t, f, 202) {
			if message.Document == nil {
				continue
			}
			count++
			body, downloadErr := f.b.TG.Download(t.Context(), *message.Document)
			require.NoError(t, downloadErr)
			files[message.Document.Filename] = body
		}
		return files, count
	}
	handleVisible(t, f.b, message(8730, 202, "/exportfoodorders"))
	files, count := visibleFiles()
	require.Equal(t, 2, count, "both CSV files must be delivered before replay")
	assert.Equal(t, expected.Orders, files["food_orders_food-bot.csv"])
	assert.Equal(t, expected.Summary, files["meal_summary_food-bot.csv"])
	assert.Empty(t, exportDocuments(t, f, 202))
	handleVisible(t, f.b, message(8730, 202, "/exportfoodorders"))
	replayed, replayCount := visibleFiles()
	require.Equal(t, count, replayCount, "completed update replay must not duplicate CSV delivery")
	assert.Equal(t, files, replayed, "completed update replay must preserve CSV bytes")
	assert.Empty(t, exportDocuments(t, f, 202))
	err = f.b.Handle(t.Context(), message(8731, 101, "/exportfoodorders"))
	require.EqualError(t, err, "food export unavailable")
	pumpBotDeliveries(t, f.b)
	for _, message := range chatMessages(t, f, 101) {
		assert.Nil(t, message.Document, "owner without export role must not receive files")
	}
}
