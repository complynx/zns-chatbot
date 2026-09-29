package appclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
)

func TestDerivedOwnerMismatchRejectsBeforeOperation(t *testing.T) {
	t.Parallel()
	host := Host{
		UserToken: func(context.Context, string) (string, error) { return "bob-token", nil },
		LocalDerived: &LocalDerived{Authorizer: applicationauth.Authorizer{
			DB:     permittedOrderOwner{},
			Verify: func(context.Context, string) (string, error) { return "bob", nil },
		}},
	}
	called := false
	value, err := directDerived(t.Context(), host, "alice", func(derivedmutation.Service, string) (string, error) {
		called = true
		return "private result", nil
	})
	require.False(t, called)
	require.Empty(t, value)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusUnauthorized, problem.Status)
	require.Equal(t, "unauthorized", problem.Code)
}
