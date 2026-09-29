package applicationauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type ownerQuery struct {
	calls  int
	exists bool
	err    error
	owner  string
}

func (q *ownerQuery) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.calls++
	q.owner = args[0].(string)
	return q
}
func (q *ownerQuery) Scan(dest ...any) error {
	if q.err != nil {
		return q.err
	}
	*(dest[0].(*bool)) = q.exists
	return nil
}

func TestAuthorizeFailureBoundaries(t *testing.T) {
	t.Parallel()
	providerErr := errors.New("identity provider unavailable")
	databaseErr := errors.New("database unavailable")
	for _, test := range []struct {
		name, token, owner     string
		verifyErr, dbErr, want error
		known                  bool
		calls                  int
	}{
		{name: "missing", want: applicationauth.ErrUnauthorized},
		{name: "invalid", token: "bad", verifyErr: identity.ErrZitadelIdentity, want: applicationauth.ErrUnauthorized},
		{name: "inactive user", token: "token", verifyErr: identity.ErrZitadelUserInactive, want: applicationauth.ErrUnauthorized},
		{name: "invalid sandbox", token: "bad", verifyErr: identity.ErrSandboxIdentity, want: applicationauth.ErrUnauthorized},
		{name: "provider outage", token: "token", verifyErr: providerErr, want: providerErr},
		{name: "canceled", token: "token", verifyErr: context.Canceled, want: context.Canceled},
		{name: "deadline", token: "token", verifyErr: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "empty owner", token: "token", want: applicationauth.ErrUnauthorized},
		{name: "unknown owner", token: "token", owner: "missing", want: applicationauth.ErrForbidden, calls: 1},
		{name: "database outage", token: "token", owner: "alice", dbErr: databaseErr, want: databaseErr, calls: 1},
		{name: "valid", token: "token", owner: "alice", known: true, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query := &ownerQuery{exists: test.known, err: test.dbErr}
			a := applicationauth.Authorizer{
				DB:     query,
				Verify: func(context.Context, string) (string, error) { return test.owner, test.verifyErr },
			}
			principal, err := a.Authorize(t.Context(), test.token)
			if test.want == nil {
				require.NoError(t, err)
				require.Equal(t, test.owner, principal.Owner())
			} else {
				require.ErrorIs(t, err, test.want)
				require.Empty(t, principal.Owner())
			}
			require.Equal(t, test.calls, query.calls)
		})
	}
}

func TestAuthorizeRefreshesProviderAndMembership(t *testing.T) {
	t.Parallel()
	query := &ownerQuery{exists: true}
	calls := 0
	a := applicationauth.Authorizer{
		DB:     query,
		Verify: func(context.Context, string) (string, error) { calls++; return "alice", nil },
	}
	principal, err := a.Authorize(t.Context(), "token")
	require.NoError(t, err)
	require.Equal(t, "alice", principal.Owner())
	query.exists = false
	_, err = a.Authorize(t.Context(), "token")
	require.ErrorIs(t, err, applicationauth.ErrForbidden)
	require.Equal(t, 2, calls)
	require.Equal(t, 2, query.calls)
	require.Equal(t, "alice", query.owner)
}
