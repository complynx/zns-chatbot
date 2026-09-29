package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFoodReceiptProposalProviderContract(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, kind string
		valid      bool
	}{
		{"modern", "", true}, {"meals", "meals", true},
		{"activities", "activities", true}, {"unknown", "paid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			proposal := MediaProposal{
				MediaID:  "media",
				Intent:   mediaReceipt,
				OrderID:  "food-order",
				FoodKind: test.kind,
			}
			raw, err := json.Marshal(proposal)
			require.NoError(t, err)
			require.NoError(t, validatePlanFields(raw, planMediaField))
			require.NoError(t, codexMediaFields(raw))
			err = validateMediaProposal(Plan{View: MediaView, MediaAction: &proposal})
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	// Command coordinates remain host-owned rather than model output fields.
	require.Error(t, validatePlanFields([]byte(`{"media_id":"media","generation":3}`), planMediaField))
}
