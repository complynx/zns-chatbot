package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func newModernBoundaryDraft(t *testing.T, f *fixture, update int64) string {
	t.Helper()
	result := runModernContinuation(t, f, update, identity.AliceTelegramID, "Prepare a new order",
		`return tools.orders.choice({operation:"begin",empty:true});`)
	var draft struct {
		Ref string `json:"choice_ref"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	require.NotEmpty(t, draft.Ref)
	return draft.Ref
}

func assertModernBoundaryDenied(t *testing.T, f *fixture, update, user int64, expression string) {
	t.Helper()
	result := runModernContinuation(t, f, update, user, "Check draft boundary",
		"let denied=false;try{"+expression+"}catch(_){denied=true}return {denied};")
	assert.JSONEq(t, `{"denied":true}`, string(result))
}

func TestModernChoiceRevisionExpiryAndPatchLimits(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ref := newModernBoundaryDraft(t, f, 51000)
	assertModernBoundaryDenied(t, f, 51001, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.choice({operation:"patch",choice_ref:%q,event:"sandbox-festival",customer:"rejected"});`,
		ref,
	))
	// Failed validation must not consume the parent revision.
	result := runModernContinuation(t, f, 51002, identity.AliceTelegramID, "Change draft customer", fmt.Sprintf(
		`return tools.orders.choice({operation:"patch",choice_ref:%q,customer:"next"});`, ref))
	var next struct {
		Ref string `json:"choice_ref"`
	}
	require.NoError(t, json.Unmarshal(result, &next))
	require.NotEmpty(t, next.Ref)
	assertModernBoundaryDenied(t, f, 51003, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.choice({operation:"patch",choice_ref:%q,customer:"branch"});`, ref))
	assertModernBoundaryDenied(t, f, 51004, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.update({name:"create",choice_ref:%q});`, ref))
	assertModernBoundaryDenied(t, f, 51005, identity.BobTelegramID, fmt.Sprintf(
		`tools.orders.choice({operation:"read",choice_ref:%q});`, next.Ref))
	assertModernBoundaryDenied(t, f, 51006, identity.AliceTelegramID,
		`tools.orders.choice({operation:"read",choice_ref:"-1.0.0"});`)
	// Seed the terminal receipt depth instead of creating 128 identical patches.
	_, err := f.db.Exec(t.Context(), `UPDATE bot.interactions
 SET content=jsonb_set(content,'{0,calls,0,modern_choice,depth}','128')
 WHERE owner='alice' AND update_id=51002 AND kind='script_runs'`)
	require.NoError(t, err)
	assertModernBoundaryDenied(t, f, 51007, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.choice({operation:"patch",choice_ref:%q,customer:"overflow"});`, next.Ref))
	_, err = f.db.Exec(t.Context(), `UPDATE bot.interactions SET created_at=clock_timestamp()-interval '25 hours'
 WHERE owner='alice' AND update_id=51002 AND kind='script_runs'`)
	require.NoError(t, err)
	assertModernBoundaryDenied(t, f, 51008, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.update({name:"create",choice_ref:%q});`, next.Ref))
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders`).Scan(&count))
	assert.Zero(t, count)
}

func TestModernChoiceCurrentPermissionDiscovery(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ref := newModernBoundaryDraft(t, f, 51100)
	code := fmt.Sprintf(`const bound=typeof tools.orders.choice==="function";
 const listed=tools.$list().some(t=>t.name==="orders.choice");let help=false,patched=false;
 if(bound){try{tools.orders.choice.$help();help=true}catch(_){}
 if(!listed){try{tools.orders.choice({operation:"patch",choice_ref:%q,customer:"revoked"});patched=true}catch(_){}}}
 return {bound,listed,help,patched};`, ref)
	for index, step := range []struct {
		initial bool
		current bool
		want    string
	}{
		{true, false, `{"bound":true,"listed":false,"help":false,"patched":false}`},
		{false, true, `{"bound":false,"listed":false,"help":false,"patched":false}`},
		{true, true, `{"bound":true,"listed":true,"help":true,"patched":false}`},
	} {
		_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=$1 WHERE id='alice'`, step.initial)
		require.NoError(t, err)
		f.b.Scripts = scopeVM{before: func() {
			_, execErr := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=$1 WHERE id='alice'`, step.current)
			require.NoError(t, execErr)
		}}
		model := &knowledgeModel{plans: []agent.Plan{
			{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
			{View: agent.KnowledgeView, Text: "Checked"},
		}}
		f.b.Model = model
		handle(t, f.b, message(51101+int64(index), identity.AliceTelegramID, "Check draft tools"))
		require.Len(t, model.inputs, 2)
		require.Len(t, model.inputs[1].Script.Runs, 1)
		run := model.inputs[1].Script.Runs[0]
		require.Empty(t, run.Error)
		assert.JSONEq(t, step.want, string(run.Result))
	}
}

func TestModernChoiceConcurrentCreateClaimsOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ref := newModernBoundaryDraft(t, f, 51200)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for index := range 2 {
		group.Go(func() {
			model := &knowledgeModel{plans: []agent.Plan{
				{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{InputJSON: "null", Code: fmt.Sprintf(
					`let created=false;try{tools.orders.update({name:"create",choice_ref:%q});created=true}catch(_){}return {created};`,
					ref,
				)}},
				{View: agent.KnowledgeView, Text: "Checked"},
			}}
			current := &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: model,
				Scripts: scopeVM{before: func() {
					ready <- struct{}{}
					select {
					case <-start:
					case <-ctx.Done():
					}
				}}}
			results <- current.Handle(ctx, message(51201+int64(index), identity.AliceTelegramID, "Create draft order"))
		})
	}
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("concurrent workers did not reach admission barrier")
		}
	}
	close(start)
	group.Wait()
	for range 2 {
		require.NoError(t, <-results)
	}
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestModernChoiceCatalogRaceAtAPITransaction(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ref := newModernBoundaryDraft(t, f, 51300)
	called := false
	f.b.API.HTTP = &http.Client{Transport: modernRevokeTransport{before: func(ctx context.Context) error {
		called = true
		_, err := f.db.Exec(ctx, `UPDATE core.order_events SET deadline=deadline+interval '1 second'
 WHERE id='sandbox-festival'`)
		return err
	}}}
	f.b.Host.HTTP = f.b.API.HTTP
	assertModernBoundaryDenied(t, f, 51301, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.update({name:"create",choice_ref:%q});`, ref))
	assert.True(t, called, "catalog changes after host preparation, before the API transaction")
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders`).Scan(&count))
	assert.Zero(t, count)
}

func TestModernChoiceStaleOrderAndPaymentSnapshot(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"version", "attempt"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			service := orders.Service{DB: f.db}
			order, err := service.Execute(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival",
				Name:    "create",
				Origin:  "manual",
				Key:     "seed",
				Choice:  orderChoice("preparty"),
			})
			require.NoError(t, err)
			cash := orderCommand("cash", order)
			cash.PaymentAdmin = "bob"
			order, err = service.Execute(t.Context(), "alice", cash)
			require.NoError(t, err)
			result := runModernContinuation(t, f, 51400, identity.AliceTelegramID, "Prepare order edit", fmt.Sprintf(
				`tools.orders.inspect({order_id:%q});return tools.orders.choice({operation:"begin",order_id:%q});`,
				order.ID,
				order.ID,
			))
			var draft struct {
				Ref string `json:"choice_ref"`
			}
			require.NoError(t, json.Unmarshal(result, &draft))
			query := `UPDATE core.orders SET version=version+1 WHERE id=$1`
			if mutation == "attempt" {
				query = `UPDATE core.orders SET attempt='replacement-payment' WHERE id=$1`
			}
			_, err = f.db.Exec(t.Context(), query, order.ID)
			require.NoError(t, err)
			assertModernBoundaryDenied(t, f, 51401, identity.AliceTelegramID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q});tools.orders.update({name:"edit",order_id:%q,choice_ref:%q});`,
				order.ID, order.ID, draft.Ref))
			current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, order.Choice, current.Choice)
		})
	}
}

func TestModernChoiceOversizedAPIBodyDenied(t *testing.T) {
	t.Parallel()
	f := setup(t)
	choice := orders.ChoiceInput{Customer: strings.Repeat("x", 321<<10)}
	_, err := f.b.API.QuoteOrder(t.Context(), "alice", "sandbox-festival", choice)
	requireCode(t, err, "invalid_json")
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "oversized", Choice: &choice,
	})
	requireCode(t, err, "invalid_json")
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders`).Scan(&count))
	assert.Zero(t, count)
}
