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

type registrationExchange struct{ calls int }

func (e *registrationExchange) Exchange(_ context.Context, subject string) (string, error) {
	e.calls++
	return subject, nil
}

type registrationLinks struct{}

func (registrationLinks) Telegram(context.Context, int64) (identity.User, error) {
	return identity.User{Owner: "alice", Subject: "subject-alice"}, nil
}

func TestLocalRegistrationAuthenticatesEveryOperation(t *testing.T) {
	t.Parallel()
	exchange := &registrationExchange{}
	failure := error(nil)
	verifications := 0
	c := Client{Exchange: exchange, Links: registrationLinks{}, LocalRegistration: &LocalRegistration{
		Authorizer: applicationauth.Authorizer{DB: permittedOrderOwner{},
			Verify: func(_ context.Context, token string) (string, error) {
				verifications++
				require.Equal(t, "subject-alice", token)
				return "alice", failure
			}},
	}}
	ctx, owner, err := c.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	_, err = c.ExecutePassBooking(ctx, owner, passbooking.Command{})
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusBadRequest, problem.Status)
	// A later provider rejection must prevent even a read reaching the service.
	failure = identity.ErrZitadelIdentity
	_, err = c.PassBooking(ctx, owner, "dance")
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusUnauthorized, problem.Status)
	require.Equal(t, 2, exchange.calls)
	require.Equal(t, 2, verifications)
	_, err = c.PassBooking(ctx, "bob", "dance")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	require.Equal(t, 2, exchange.calls)
}

func TestLocalRegistrationAuthorizationFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		owner   string
		failure error
		status  int
	}{
		{"owner", "bob", nil, http.StatusForbidden},
		{"provider", "alice", errors.New("private provider detail"), http.StatusInternalServerError},
		{"canceled", "alice", context.Canceled, 0},
		{"deadline", "alice", context.DeadlineExceeded, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := Client{SandboxToken: (identity.Signer{}).Token, LocalRegistration: &LocalRegistration{
				Authorizer: applicationauth.Authorizer{DB: permittedOrderOwner{},
					Verify: func(context.Context, string) (string, error) { return test.owner, test.failure }},
			}}
			for _, command := range []bool{false, true} {
				var value passbooking.Booking
				var err error
				if command {
					value, err = c.ExecutePassBooking(t.Context(), "alice", passbooking.Command{})
				} else {
					value, err = c.PassBooking(t.Context(), "alice", "dance")
				}
				require.Empty(t, value)
				if test.status == 0 {
					require.ErrorIs(t, err, test.failure)
				} else {
					var problem *core.ProblemError
					require.ErrorAs(t, err, &problem)
					require.Equal(t, test.status, problem.Status)
					require.NotContains(t, err.Error(), "private")
				}
			}
		})
	}
}

func TestLocalRegistrationClockChangeRemainsResumable(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		passbooking.ErrRegistrationTimeChanged,
		errors.Join(passbooking.ErrRegistrationTimeChanged, context.Canceled),
		errors.Join(passbooking.ErrRegistrationTimeChanged, context.DeadlineExceeded),
	} {
		_, err := registrationResult(passbooking.Booking{}, failure)
		require.ErrorIs(t, err, passbooking.ErrRegistrationTimeChanged)
		var problem *core.ProblemError
		require.NotErrorAs(t, err, &problem)
	}
	failure := errors.Join(passbooking.ErrRegistrationTimeChanged, core.ErrDatabase)
	_, err := registrationResult(passbooking.Booking{}, failure)
	require.True(t, core.IsDatabaseFailure(err), "positive SQL failure remains fatal")
}
