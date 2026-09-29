package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBusinessDiscoveryUsesFreshCapabilities(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"missing", "restricted", "ordinary", "exporter", "revoked"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			input := Input{}
			if role != "missing" {
				input.Business = &BusinessCapabilities{
					CanBook:         role != "restricted",
					CanExportOrders: role == "exporter" || role == "revoked",
				}
			}
			checks, calls := 0, 0
			input.BeforeProvider = func(_ context.Context, current *Input) error {
				checks++
				if role == "revoked" && checks == 2 {
					current.Business = &BusinessCapabilities{}
				}
				return nil
			}
			_, err := planWithSkills(
				t.Context(),
				input,
				func(_ context.Context, prompt providerPrompt) (string, error) {
					calls++
					if calls == 1 {
						return `{"skills":["booking","orders"],"reply_language":"en"}`, nil
					}
					checkBusinessPrompt(t, role, prompt)
					return emptyActionsPlan, nil
				},
			)
			require.NoError(t, err)
			assert.Equal(t, 2, calls)
			assert.Equal(t, 2, checks)
		})
	}
}

func TestGuessedBusinessActionsNeedCapabilities(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"create", "add_extra", "remove_extra", "export", orderPaymentInstructions} {
		require.Error(t, validateVisiblePlan(Input{}, Plan{OrderAction: &OrderProposal{Name: name}}))
	}
	require.Error(t, validateVisiblePlan(Input{}, Plan{Action: &Proposal{Name: "select"}}))
	input := Input{Business: &BusinessCapabilities{CanBook: true}}
	require.NoError(t, validateVisiblePlan(input, Plan{OrderAction: &OrderProposal{Name: orderPaymentInstructions}}))
	require.Error(t, validateVisiblePlan(input, Plan{OrderAction: &OrderProposal{Name: "export"}}))
	require.NoError(t, validateVisiblePlan(input, Plan{OrderAction: &OrderProposal{Name: "create"}}))
}

func checkBusinessPrompt(t *testing.T, role string, prompt providerPrompt) {
	t.Helper()
	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(prompt.schema), &schema))
	properties, ok := schema[schemaProperties].(map[string]any)
	require.True(t, ok)
	orderFields := actionProperties(properties, "order_action")
	allowed := role == "ordinary" || role == "exporter"
	var names any = []any{}
	if allowed {
		require.NotNil(t, orderFields)
		name, found := orderFields["name"].(map[string]any)
		require.True(t, found)
		names = name[schemaEnum]
		assert.Contains(t, names, orderPaymentInstructions)
		assert.Contains(t, prompt.instructions, orderPaymentInstructions)
		assert.Contains(t, names, "create")
		assert.Contains(t, prompt.instructions, "action select")
	} else {
		assert.Equal(t, map[string]any{schemaType: "null"}, properties["order_action"])
		assert.NotContains(t, prompt.instructions, orderPaymentInstructions)
		assert.Equal(t, map[string]any{schemaType: "null"}, properties["action"])
		for _, hidden := range []string{"create", "add_extra", "remove_extra"} {
			assert.NotContains(t, names, hidden)
		}
		assert.NotContains(t, prompt.instructions, "action select")
		assert.NotContains(t, prompt.instructions, "add_extra")
	}
	if role == "exporter" {
		assert.Contains(t, names, "export")
		assert.Contains(t, prompt.instructions, "order_action export")
	} else {
		assert.NotContains(t, names, "export")
		assert.NotContains(t, prompt.instructions, "order_action export")
	}
}
