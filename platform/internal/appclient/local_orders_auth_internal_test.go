package appclient

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestDirectOrderVerificationRecoversWithoutEffect(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		failure error
		status  int
	}{
		{"provider", errors.New("provider secret"), http.StatusInternalServerError},
		{"denied", identity.ErrZitadelIdentity, http.StatusUnauthorized},
		{"canceled", context.Canceled, 0},
		{"deadline", context.DeadlineExceeded, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			failure := test.failure
			verifications, effects := 0, 0
			client := Client{LocalOrders: &LocalOrders{Authorizer: applicationauth.Authorizer{
				DB: permittedOrderOwner{},
				Verify: func(context.Context, string) (string, error) {
					verifications++
					return "alice", failure
				},
			}}, SandboxToken: (identity.Signer{}).
				Token,
			}
			operation := func(orders.Service, string) (string, error) {
				effects++
				return "committed", nil
			}
			value, err := directOrder(t.Context(), client, "alice", operation)
			require.Empty(t, value)
			if test.status == 0 {
				require.ErrorIs(t, err, test.failure)
			} else {
				var problem *core.ProblemError
				require.ErrorAs(t, err, &problem)
				require.Equal(t, test.status, problem.Status)
				require.NotContains(t, err.Error(), "secret")
			}
			require.Zero(t, effects)
			failure = nil
			value, err = directOrder(t.Context(), client, "alice", operation)
			require.NoError(t, err)
			require.Equal(t, "committed", value)
			require.Equal(t, 1, effects)
			require.Equal(t, 2, verifications)
		})
	}
}
