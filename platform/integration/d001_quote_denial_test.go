package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestD001PublicQuoteDenial(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"catalog", "order"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			service := orders.Service{DB: f.db}
			original, err := service.Execute(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "d001-public-quote",
				Choice: &orders.ChoiceInput{Customer: "Synthetic initial choice"},
			})
			require.NoError(t, err)
			result := runModernContinuation(t, f, 94101, identity.AliceTelegramID, "Prepare quote", fmt.Sprintf(
				`tools.orders.inspect({order_id:%q});return tools.orders.choice({operation:"begin",order_id:%q});`,
				original.ID, original.ID,
			))
			var draft struct {
				Ref string `json:"choice_ref"`
			}
			require.NoError(t, json.Unmarshal(result, &draft))
			require.NotEmpty(t, draft.Ref)
			if scenario == "catalog" {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE core.order_events SET deadline=deadline+interval '1 second' WHERE id=$1`,
					original.EventID,
				)
				require.NoError(t, err)
			} else {
				command := orderCommand("edit", original)
				choice := original.Choice.Input()
				choice.Customer = "Synthetic newer manual choice"
				command.Choice = &choice
				original, err = service.Execute(t.Context(), "alice", command)
				require.NoError(t, err)
			}
			assertModernBoundaryDenied(t, f, 94102, identity.AliceTelegramID,
				fmt.Sprintf(`tools.orders.quote({choice_ref:%q});`, draft.Ref))
			current, err := service.Get(t.Context(), "alice", original.EventID, original.ID)
			require.NoError(t, err)
			require.Equal(t, original.Version, current.Version)
			require.Equal(t, original.Choice, current.Choice)
		})
	}
}
