package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRemoteWireUsesFreshVisibilityAndRejectsGuessedAction(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"ordinary", "current", "revoked-before", "revoked-during"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input := capabilityTestInput("payment")
			input.Registration.Reads = []RegistrationReadResult{{
				Request:      RegistrationProposal{Name: RegistrationRead, Event: "a", View: "payment_queue"},
				PaymentQueue: []passbooking.PaymentReview{{Owner: "private-review-target"}},
			}}
			checks := 0
			input.BeforeProvider = func(_ context.Context, current *Input) error {
				checks++
				if scenario == "ordinary" || scenario == "revoked-before" ||
					scenario == "revoked-during" && checks == 2 {
					current.Registration.Capabilities = nil
				}
				return nil
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, 1, checks)
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				if scenario == "ordinary" || scenario == "revoked-before" {
					assert.NotContains(t, string(body), "proof_accept")
					assert.NotContains(t, string(body), "payment_queue")
					assert.NotContains(t, string(body), "private-review-target")
				} else {
					assert.Contains(t, string(body), "proof_accept")
					assert.Contains(t, string(body), "private-review-target")
				}
				assert.NoError(
					t,
					json.NewEncoder(w).
						Encode(Plan{View: RegistrationView, RegistrationAction: &RegistrationProposal{Name: "proof_accept", Event: "a", Target: "private-review-target"}}),
				)
			}))
			defer server.Close()
			_, err := (Remote{URL: server.URL, HTTP: server.Client()}).Plan(t.Context(), input)
			if scenario == "current" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "capability unavailable")
			}
			assert.Equal(t, 1, calls)
			assert.Equal(t, 2, checks)
		})
	}
}

func TestRemoteAuthorizationFailurePreventsRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { t.Error("request must not run") }),
	)
	defer server.Close()
	input := Input{BeforeProvider: func(context.Context, *Input) error { return errors.New("access revoked") }}
	_, err := (Remote{URL: server.URL, HTTP: server.Client()}).Plan(t.Context(), input)
	require.ErrorContains(t, err, "access revoked")
}
