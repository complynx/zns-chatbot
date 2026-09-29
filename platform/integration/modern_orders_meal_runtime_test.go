package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestModernChoiceMealRuntime(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = startModernRuntimeWorker(t)
	f.b.Model = sandbox.FixtureRemote{URL: f.fake.URL + "/lab/model"}
	f.b.WebAppURL = "https://sandbox.invalid/orders"
	name := strings.Repeat("dish", 50000)
	menu := orders.Catalog{
		Dishes:  map[string]orders.Definition{name: {Price: 100}},
		Choices: map[string]map[string]map[string][]string{"day": {"meal": {"category": {name}}}},
	}
	raw, err := json.Marshal(menu)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.order_events SET menu=$1,extras='{}'::jsonb WHERE id='sandbox-festival'`,
		raw,
	)
	require.NoError(t, err)
	result := modernRuntimeScript(t, f, "Create the selected meal order", `
 let d=tools.orders.choice({operation:"begin",empty:true});
 const catalog=tools.orders.choice({operation:"read",choice_ref:d.choice_ref,part:"catalog"});
 d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,meals:[{day_ref:0,meal_ref:0,dishes:[{ref:0,count:1}]}]});
 return {ref:d.choice_ref,bytes:d.choice_bytes};`)
	var draft struct {
		Ref   string `json:"ref"`
		Bytes int    `json:"bytes"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	assert.Greater(t, draft.Bytes, 128<<10)
	result = modernRuntimeScript(
		t,
		f,
		"Commit the full selected meal order",
		fmt.Sprintf(`return tools.orders.update({name:"create",choice_ref:%q});`, draft.Ref),
	)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(result, &created))
	service := orders.Service{DB: f.db}
	order, err := service.Get(t.Context(), "alice", "sandbox-festival", created.ID)
	require.NoError(t, err)
	require.Len(t, order.Choice.Days["day"].Mealtimes["meal"].Dishes, 1)
	assert.Equal(t, name, order.Choice.Days["day"].Mealtimes["meal"].Dishes[0].Name)
}
