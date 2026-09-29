package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModernOrdersProviderPromptUsesCurrentReviewCapability(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"ordinary", "administrator", "granted", "revoked"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			input := Input{Business: &BusinessCapabilities{
				CanBook: true, CanExportOrders: role == "administrator" || role == "revoked",
			}}
			checks, calls := 0, 0
			input.BeforeProvider = func(_ context.Context, current *Input) error {
				checks++
				if checks == 2 {
					current.Business.CanExportOrders = role == "administrator" || role == "granted"
				}
				return nil
			}
			_, err := planWithSkills(t.Context(), input, func(
				_ context.Context, prompt providerPrompt,
			) (string, error) {
				calls++
				if calls == 1 {
					return `{"skills":["orders"],"reply_language":"en"}`, nil
				}
				checkModernOrdersPrompt(t, prompt.instructions, role == "administrator" || role == "granted")
				return emptyActionsPlan, nil
			})
			require.NoError(t, err)
			assert.Equal(t, 2, checks)
			assert.Equal(t, 2, calls)
		})
	}
}

func checkModernOrdersPrompt(t *testing.T, instructions string, administrator bool) {
	t.Helper()
	for _, name := range []string{
		"orders.inbox", "orders.review.read", "orders.review.proof", "orders.review.decide", "orders.export",
	} {
		if administrator {
			assert.Contains(t, instructions, name)
		} else {
			assert.NotContains(t, instructions, name)
		}
	}
	assert.Contains(t, instructions, "orders.inspect")
	assert.Contains(t, instructions, "resume=true")
	assert.Contains(t, instructions, "Unicode rune index")
	assert.Contains(t, instructions, "replay the completed final page")
}
