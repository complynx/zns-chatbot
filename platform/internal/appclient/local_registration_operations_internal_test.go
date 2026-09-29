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
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestLocalRegistrationOperationsAuthorization(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, owner string
		failure     error
		status      int
	}{
		{"owner", "bob", nil, http.StatusForbidden},
		{"inactive", "alice", identity.ErrZitadelIdentity, http.StatusUnauthorized},
		{"provider", "alice", errors.New("private provider detail"), http.StatusInternalServerError},
		{"canceled", "alice", context.Canceled, 0},
		{"deadline", "alice", context.DeadlineExceeded, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			exchange := &registrationExchange{}
			verified := 0
			c := Client{Exchange: exchange, Links: registrationLinks{}, LocalRegistration: &LocalRegistration{
				Authorizer: applicationauth.Authorizer{
					DB: permittedOrderOwner{},
					Verify: func(context.Context, string) (string, error) {
						verified++
						return test.owner, test.failure
					},
				},
			}}
			ctx, owner, err := c.AuthenticateTelegram(t.Context(), 101)
			require.NoError(t, err)
			reads := []func(context.Context, string) error{
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassCapabilities(ctx, owner, "dance")
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassAdminTarget(ctx, owner, "dance", 101)
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.AssignPass(ctx, owner, passbooking.AdminAssignment{})
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassTakeoverTarget(ctx, owner, "dance", 101)
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassPayment(ctx, owner, "dance", "alice")
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassPaymentQuote(ctx, owner, "dance")
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassPaymentQueue(ctx, owner, "dance", "")
					require.Empty(t, value)
					return readErr
				},
			}
			for _, read := range reads {
				err = read(ctx, owner)
				if test.status == 0 {
					require.ErrorIs(t, err, test.failure)
				} else {
					var problem *core.ProblemError
					require.ErrorAs(t, err, &problem)
					require.Equal(t, test.status, problem.Status)
					require.NotContains(t, err.Error(), "private")
				}
				require.ErrorIs(t, read(ctx, "foreign-owner"), identity.ErrZitadelIdentity)
			}
			require.Equal(t, len(reads), exchange.calls)
			require.Equal(t, len(reads), verified)
		})
	}
}

func TestDirectRegistrationErrorBoundary(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{errors.New("private storage detail"), context.Canceled, context.DeadlineExceeded,
		&core.ProblemError{Status: http.StatusConflict, Code: "pass_booking_stale"},
	} {
		c := Client{SandboxToken: (identity.Signer{}).Token, LocalRegistration: &LocalRegistration{
			Authorizer: applicationauth.Authorizer{
				DB:     permittedOrderOwner{},
				Verify: func(context.Context, string) (string, error) { return "alice", nil },
			},
		}}
		value, err := directRegistration(
			t.Context(),
			c,
			"alice",
			func(passbooking.Service, string) (string, error) { return "private partial result", failure },
		)
		require.Empty(t, value)
		require.NotContains(t, err.Error(), "private")
		if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
			require.ErrorIs(t, err, failure)
		} else {
			var problem *core.ProblemError
			require.ErrorAs(t, err, &problem)
			if known, ok := errors.AsType[*core.ProblemError](failure); ok {
				require.Same(t, known, problem)
			} else {
				require.Equal(t, "internal_error", problem.Code)
			}
		}
	}
}
