package appclient

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestDirectOrderErrorsPreservePublicBoundary(t *testing.T) {
	t.Parallel()
	problem := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	for _, err := range []error{nil, context.Canceled, context.DeadlineExceeded, problem} {
		require.Equal(t, err, orderApplicationError(err))
	}
	err := orderApplicationError(errors.New("database password or SQL detail"))
	var public *core.ProblemError
	require.ErrorAs(t, err, &public)
	require.Equal(t, http.StatusInternalServerError, public.Status)
	require.Equal(t, "internal_error", public.Code)
	require.NotContains(t, err.Error(), "database")
}

type permittedOrderOwner struct{}

func (permittedOrderOwner) QueryRow(context.Context, string, ...any) pgx.Row {
	return permittedOrderOwner{}
}
func (permittedOrderOwner) Scan(dest ...any) error { *(dest[0].(*bool)) = true; return nil }

func TestDirectOrderFailureDoesNotReturnPartialValue(t *testing.T) {
	t.Parallel()
	c := Client{
		LocalOrders: &LocalOrders{
			Authorizer: applicationauth.Authorizer{
				DB:     permittedOrderOwner{},
				Verify: func(context.Context, string) (string, error) { return "alice", nil },
			},
		}, SandboxToken: (identity.Signer{}).
			Token,
	}
	value, err := directOrder(t.Context(), c, "alice", func(orders.Service, string) (string, error) {
		return "partial private result", errors.New("database failure")
	})
	require.Empty(t, value)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "internal_error", problem.Code)
}
