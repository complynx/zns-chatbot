package integration_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func completeModernChoiceRead(t *testing.T, f *fixture, id string, update int64) {
	t.Helper()
	more := true
	for turn := 0; more && turn < 24; turn++ {
		result := runModernContinuation(
			t,
			f,
			update+int64(turn),
			identity.AliceTelegramID,
			"Inspect order "+id,
			fmt.Sprintf(
				`let p=tools.orders.inspect({order_id:%q,resume:%t});for(let i=1;i<8&&p.more;i++)p=tools.orders.inspect({order_id:%q,cursor:p.next_cursor});return {more:p.more};`,
				id,
				turn > 0,
				id,
			),
		)
		var state struct {
			More bool `json:"more"`
		}
		require.NoError(t, json.Unmarshal(result, &state))
		more = state.More
	}
	require.False(t, more)
}

func TestModernChoiceLargeCreateEditReplace(t *testing.T) {
	t.Parallel()
	f, s, original := modernLargeOrder(t, false)
	completeModernChoiceRead(t, f, original.ID, 46000)
	result := runModernContinuation(
		t,
		f,
		46100,
		identity.AliceTelegramID,
		"Change the customer name of order "+original.ID,
		fmt.Sprintf(
			`tools.orders.inspect({order_id:%q,resume:true});const d=tools.orders.choice({operation:"begin",order_id:%q});const p=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:"Y"});return {ref:p.choice_ref};`,
			original.ID,
			original.ID,
		),
	)
	var draft struct {
		Ref string `json:"ref"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	require.NotEmpty(t, draft.Ref)
	result = runModernContinuation(
		t,
		f,
		46101,
		identity.AliceTelegramID,
		"Apply the name edit to order "+original.ID,
		fmt.Sprintf(
			`tools.orders.inspect({order_id:%q,resume:true});const quote=tools.orders.quote({choice_ref:%q});const edited=tools.orders.update({name:"edit",order_id:%q,choice_ref:%q});return {id:edited.id,more:quote.more};`,
			original.ID,
			draft.Ref,
			original.ID,
			draft.Ref,
		),
	)
	t.Logf("large edit=%s", result)
	edited, err := s.Get(t.Context(), "alice", original.EventID, original.ID)
	require.NoError(t, err)
	assert.Equal(t, "Y", edited.Choice.Customer)
	assert.Equal(t, original.Choice.Extras, edited.Choice.Extras)
	// A consumed revision cannot create or branch again.
	result = runModernContinuation(t, f, 46102, identity.AliceTelegramID, "Check draft status", fmt.Sprintf(
		`let denied=false;try{tools.orders.choice({operation:"patch",choice_ref:%q,customer:"Z"})}catch(_){denied=true}return {denied};`,
		draft.Ref,
	))
	assert.JSONEq(t, `{"denied":true}`, string(result))
	// Build an arbitrary complete selection from bounded catalog references.
	result = runModernContinuation(t, f, 46200, identity.AliceTelegramID, "Create a separate full order", `
 let d=tools.orders.choice({operation:"begin",empty:true});
 for(let start=0;start<259;start+=64){const extras=[];for(let i=start;i<Math.min(start+64,259);i++)extras.push({ref:i,selected:true});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,extras});}
 return {ref:d.choice_ref};`)
	require.NoError(t, json.Unmarshal(result, &draft))
	result = runModernContinuation(t, f, 46201, identity.AliceTelegramID, "Create the selected full order", fmt.Sprintf(
		`const o=tools.orders.update({name:"create",choice_ref:%q});return {id:o.id};`, draft.Ref))
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(result, &created))
	require.NotEmpty(t, created.ID)
	current, err := s.Get(t.Context(), "alice", original.EventID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, original.Choice.Extras, current.Choice.Extras)
	completeModernChoiceRead(t, f, created.ID, 46300)
	result = runModernContinuation(
		t,
		f,
		46400,
		identity.AliceTelegramID,
		"Replace the whole choice of order "+created.ID,
		fmt.Sprintf(
			`tools.orders.inspect({order_id:%q,resume:true});let d=tools.orders.choice({operation:"begin",order_id:%q,empty:true});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:"Replacement"});return tools.orders.update({name:"edit",order_id:%q,choice_ref:d.choice_ref});`,
			created.ID,
			created.ID,
			created.ID,
		),
	)
	t.Logf("full replacement=%s", result)
	current, err = s.Get(t.Context(), "alice", original.EventID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Replacement", current.Choice.Customer)
	require.Len(t, current.Choice.Extras, 1)
}

func TestModernChoiceReferencesAndCatalogGuards(t *testing.T) {
	t.Parallel()
	f, s, o := modernLargeOrder(t, true)
	result := runModernContinuation(
		t,
		f,
		46500,
		identity.AliceTelegramID,
		"Prepare another order",
		`const d=tools.orders.choice({operation:"begin",empty:true});return {ref:d.choice_ref};`,
	)
	var draft struct {
		Ref string `json:"ref"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	result = runModernContinuation(t, f, 46501, identity.BobTelegramID, "Use the choice", fmt.Sprintf(
		`let denied=false;try{tools.orders.choice({operation:"read",choice_ref:%q})}catch(_){denied=true}return {denied};`,
		draft.Ref,
	))
	assert.JSONEq(t, `{"denied":true}`, string(result))
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.order_events SET deadline=deadline+interval '1 second' WHERE id=$1`,
		o.EventID,
	)
	require.NoError(t, err)
	result = runModernContinuation(t, f, 46502, identity.AliceTelegramID, "Apply the choice", fmt.Sprintf(
		`let denied=false;try{tools.orders.update({name:"create",choice_ref:%q})}catch(_){denied=true}return {denied};`,
		draft.Ref,
	))
	assert.JSONEq(t, `{"denied":true}`, string(result))
	list, err := s.ListPage(t.Context(), "alice", o.EventID, "", false)
	require.NoError(t, err)
	require.Len(t, list.Orders, 1)
}

func TestModernOrderAPIFullChoiceBody(t *testing.T) {
	t.Parallel()
	for _, multibyte := range []bool{false, true} {
		t.Run(strconv.FormatBool(multibyte), func(t *testing.T) {
			t.Parallel()
			f, s, o := modernLargeOrder(t, multibyte)
			encoded, err := json.Marshal(o.Choice)
			require.NoError(t, err)
			var choice orders.ChoiceInput
			require.NoError(t, json.Unmarshal(encoded, &choice))
			quote, err := f.b.API.QuoteOrder(t.Context(), "alice", o.EventID, choice)
			require.NoError(t, err)
			assert.Equal(t, o.Choice, quote)
			command := orderCommand("edit", o)
			command.Choice = &choice
			changed, err := f.b.API.ExecuteOrder(t.Context(), "alice", command)
			require.NoError(t, err)
			assert.Equal(t, o.Choice, changed.Choice)
			command.Name = "create"
			command.OrderID = ""
			command.Version = 0
			command.Key = "large-api-create"
			created, err := f.b.API.ExecuteOrder(t.Context(), "alice", command)
			require.NoError(t, err)
			assert.Equal(t, o.Choice, created.Choice)
			command = orderCommand("edit", changed)
			choice.Customer = strings.Repeat("x", 1025)
			command.Choice = &choice
			_, err = f.b.API.ExecuteOrder(t.Context(), "alice", command)
			requireCode(t, err, "invalid_choice")
			current, err := s.Get(t.Context(), "alice", o.EventID, o.ID)
			require.NoError(t, err)
			assert.Equal(t, changed.Version, current.Version)
		})
	}
}
