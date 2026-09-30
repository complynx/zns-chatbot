package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func runModernVM(t *testing.T, f *fixture, update, user int64, text, code string) json.RawMessage {
	t.Helper()
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Completed"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, user, text))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.NotEmpty(t, runs)
	if runs[len(runs)-1].Error != "" {
		var evidence json.RawMessage
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE update_id=$1 AND kind='script_runs'`, update).
				Scan(&evidence),
		)
		t.Log(string(evidence))
	}
	require.Empty(t, runs[len(runs)-1].Error)
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, runs[len(runs)-1].Result))
	return compact.Bytes()
}

func TestModernOrdersSobekOwnerAndAdministrator(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1`, language)
			require.NoError(t, err)
			result := runModernVM(
				t,
				f,
				38001,
				identity.AliceTelegramID,
				"Create my preparty order, correct my name, then select cash with Bob",
				`
const quote=tools.orders.quote({choice:{customer:"Daniel",days:{friday:{mealtimes:{dinner:{dishes:[{name:"caesar",count:2,price:0}]}}}},extras:{preparty:0}}});
const created=tools.orders.update({name:"create",choice:{customer:"Daniel",days:{friday:{mealtimes:{dinner:{dishes:[{name:"caesar",count:2,price:0}]}}}},extras:{preparty:0}}});
tools.orders.inspect({order_id:created.id});
tools.orders.update({name:"edit",order_id:created.id,choice:{customer:"Daniel Drizhuk",days:{friday:{mealtimes:{dinner:{dishes:[{name:"caesar",count:3,price:0}]}}}},extras:{preparty:0}}});
tools.orders.inspect({order_id:created.id});
const cash=tools.orders.update({name:"cash",order_id:created.id,contact:"bob"});
return {quote,cash};`,
			)
			assert.Contains(t, string(result), `"state":"cash"`)
			service := orders.Service{DB: f.db}
			list, err := service.List(t.Context(), "alice", "sandbox-festival")
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "Daniel Drizhuk", list[0].Choice.Customer)
			assert.EqualValues(t, 3, list[0].Choice.Days["friday"].Mealtimes["dinner"].Dishes[0].Count)
			assert.Positive(t, list[0].Choice.Days["friday"].Mealtimes["dinner"].Dishes[0].Price)
			result = runModernVM(
				t,
				f,
				38002,
				identity.BobTelegramID,
				"Accept payment for "+list[0].ID+" and export orders",
				fmt.Sprintf(`
const inbox=tools.orders.inbox({});
const read=tools.orders.review.read({order_id:%q});
const accepted=tools.orders.review.decide({order_id:%q,name:"accept"});
const exported=tools.orders.export({});
const replay=tools.orders.export({});
return {inbox,read,accepted,exported,replay};`, list[0].ID, list[0].ID),
			)
			assert.Contains(t, string(result), `"state":"paid"`)
			assert.Contains(t, string(result), `"status":"pending"`)
			drainOrderPresentations(t, f)
			assert.Len(t, exportDocuments(t, f, identity.BobTelegramID), 1)
			var deliveries int
			err = f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE kind='modern_order_delivery:orders.export' AND owner='bob'`).
				Scan(&deliveries)
			require.NoError(t, err)
			assert.Equal(t, 1, deliveries)
			result = runModernVM(
				t,
				f,
				38003,
				identity.AliceTelegramID,
				"Show my modern orders history",
				`return {history:tools.orders.history.page({}),tools:tools.$list().map(x=>x.name).filter(x=>x.startsWith("orders."))};`,
			)
			assert.NotContains(t, string(result), "orders.review")
			assert.NotContains(t, string(result), "orders.export")
		})
	}
}

func TestModernOrdersSobekProofCountryCancellation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "modern-create",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	command := orderCommand("proof", order)
	command.ProofFile = uploadProof(t, s, "alice")
	order, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	result := runModernVM(
		t,
		f,
		38101,
		identity.AliceTelegramID,
		"Show receipt for "+order.ID,
		fmt.Sprintf(
			`tools.orders.inspect({order_id:%q}); return tools.orders.proof({order_id:%q});`,
			order.ID,
			order.ID,
		),
	)
	assert.Contains(t, string(result), `"status":"pending"`)
	drainOrderPresentations(t, f)
	var deliveredProofs int
	for _, card := range chatMessages(t, f, identity.AliceTelegramID) {
		if card.Document != nil {
			deliveredProofs++
			body, downloadErr := f.b.TG.Download(t.Context(), *card.Document)
			require.NoError(t, downloadErr)
			proof, proofErr := s.OrderProof(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, proofErr)
			assert.Equal(t, proof.Body, body)
		}
	}
	require.Equal(t, 1, deliveredProofs)
	result = runModernVM(
		t, f, 38103, identity.AliceTelegramID,
		"Select Belarus and Bob, then cancel proof for "+order.ID,
		fmt.Sprintf(`
tools.orders.inspect({order_id:%q});
tools.orders.update({name:"country",order_id:%q,country:"be",contact:"bob"});
tools.orders.inspect({order_id:%q});
const cancelled=tools.orders.update({name:"cancel_proof",order_id:%q});
return {cancelled};`, order.ID, order.ID, order.ID, order.ID),
	)
	assert.Contains(t, string(result), `"state":"unpaid"`)
	assert.NotContains(t, string(result), command.ProofFile)
	order, err = s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Empty(t, order.ProofFile)
	result = runModernVM(
		t,
		f,
		38102,
		identity.AliceTelegramID,
		"Delete "+order.ID,
		fmt.Sprintf(
			`tools.orders.inspect({order_id:%q}); return tools.orders.update({name:"delete",order_id:%q});`,
			order.ID,
			order.ID,
		),
	)
	assert.Contains(t, string(result), `"state":"deleted"`)
	page, err := s.HistoryPage(t.Context(), "alice", order.EventID, "")
	require.NoError(t, err)
	assert.Len(t, page.Items, 5)
}

func TestModernOrdersProofCancelledBeforeWire(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "cancel-before-wire",
		Choice:  orderChoice("preparty"),
	})
	require.NoError(t, err)
	command := orderCommand("proof", order)
	command.ProofFile = uploadProof(t, s, "alice")
	order, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	result := runModernVM(t, f, 38104, identity.AliceTelegramID, "Show then cancel receipt for "+order.ID,
		fmt.Sprintf(`tools.orders.inspect({order_id:%q}); const shown=tools.orders.proof({order_id:%q});
const cancelled=tools.orders.update({name:"cancel_proof",order_id:%q}); return {shown,cancelled};`, order.ID, order.ID, order.ID))
	assert.Contains(t, string(result), `"status":"pending"`)
	assert.Contains(t, string(result), `"state":"unpaid"`)
	drainOrderPresentations(t, f)
	for _, card := range chatMessages(t, f, identity.AliceTelegramID) {
		assert.Nil(t, card.Document, "cancelled proof must not reach Telegram")
	}
}

func TestModernOrdersObservedPaymentReplacement(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	cash := orderCommand("cash", order)
	cash.PaymentAdmin = "bob"
	order, err = s.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			scriptCall(ctx, t, callback, "orders.review.read", fmt.Sprintf(`{"order_id":%q}`, order.ID))
			_, changeErr := s.Execute(ctx, "bob", orderCommand("reject", order))
			require.NoError(t, changeErr)
			current, readErr := s.Get(ctx, "alice", order.EventID, order.ID)
			require.NoError(t, readErr)
			cash = orderCommand("cash", current)
			cash.PaymentAdmin = "bob"
			_, changeErr = s.Execute(ctx, "alice", cash)
			require.NoError(t, changeErr)
			_, callErr := callback(
				ctx,
				scriptclient.ToolCall{
					Name:      "orders.review.decide",
					Arguments: json.RawMessage(fmt.Sprintf(`{"order_id":%q,"name":"accept"}`, order.ID)),
				},
			)
			require.Error(t, callErr)
			return json.RawMessage(`{"stale_rejected":true}`), nil
		},
	)
	f.b.Model = &knowledgeModel{
		plans: []agent.Plan{
			{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null", InputJSON: "null"}},
			{View: agent.OrdersView, Text: "Done"},
		},
	}
	handle(t, f.b, message(38201, identity.BobTelegramID, "Accept "+order.ID))
	current, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "cash", current.State)
}

func TestModernOrdersHistoryCompletePaging(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	for range 35 {
		command := orderCommand("edit", order)
		command.Choice = orderChoice("preparty")
		order, err = s.Execute(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	result := runModernVM(t, f, 38301, identity.AliceTelegramID, "Read all my modern order history", `
let cursor="",items=[]; do { const p=tools.orders.history.page({cursor}); items=items.concat(p.items); cursor=p.next_cursor; } while(cursor);
const detail=tools.orders.history.read({entry:items[0].id}); return {count:items.length,detail};`)
	assert.Contains(t, string(result), `"count":36`)
	page, err := s.HistoryPage(t.Context(), "alice", order.EventID, "")
	require.NoError(t, err)
	_, err = s.HistoryPage(t.Context(), "bob", order.EventID, page.NextCursor)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "read_cursor_invalid", problem.Code)
	var legacyID string
	err = f.db.QueryRow(t.Context(), `INSERT INTO core.order_audit(order_id,actor,origin,action,version) VALUES($1,'alice','manual','edit',37) RETURNING id::text`, order.ID).
		Scan(&legacyID)
	require.NoError(t, err)
	page, err = s.HistoryPage(t.Context(), "alice", order.EventID, "")
	require.NoError(t, err)
	assert.Equal(t, legacyID, page.Items[0].ID)
	detail, err := s.HistoryDetail(t.Context(), "alice", order.EventID, legacyID, "")
	require.NoError(t, err)
	assert.Contains(t, detail.JSON, `"details_available":false`)
}
