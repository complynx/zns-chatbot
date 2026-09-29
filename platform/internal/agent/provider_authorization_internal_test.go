package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderAuthorizationReserializesEachRequest(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"refresh", "error", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			checks, calls := 0, 0
			denied := errors.New("authorization unavailable")
			input := Input{Text: "cached private context"}
			input.BeforeProvider = func(_ context.Context, value *Input) error {
				checks++
				if checks != 2 {
					return nil
				}
				switch scenario {
				case "error":
					return denied
				case "cancel":
					cancel()
				}
				value.Text = "authorized projection"
				return nil
			}
			_, err := planWithSkills(ctx, input, func(_ context.Context, prompt providerPrompt) (string, error) {
				calls++
				assert.NotContains(t, string(prompt.input), "BeforeProvider")
				if calls == 1 {
					assert.Contains(t, string(prompt.input), "cached private context")
					return `{"skills":[],"reply_language":"en"}`, nil
				}
				assert.NotContains(t, string(prompt.input), "cached private context")
				assert.Equal(t, "authorized projection", prompt.source.Text)
				return emptyActionsPlan, nil
			})
			assert.Equal(t, 2, checks)
			if scenario == "refresh" {
				require.NoError(t, err)
				assert.Equal(t, 2, calls)
			} else {
				require.Error(t, err)
				assert.Equal(t, 1, calls)
			}
		})
	}
}
