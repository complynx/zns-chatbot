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
)

func TestLocalRegistrationReadsAuthorization(t *testing.T) {
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
					value, readErr := c.PassEvents(ctx, owner)
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassInvitations(ctx, owner, "dance", "")
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassPaymentAdmins(ctx, owner, "dance")
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.PassQueue(ctx, owner, "dance", "")
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
