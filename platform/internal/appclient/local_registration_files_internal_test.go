package appclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestLocalRegistrationFilesAuthorization(t *testing.T) {
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
					value, readErr := c.UploadPassProof(ctx, owner, "synthetic.txt", []byte("synthetic"))
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.DownloadPassProof(ctx, owner, "dance", "alice")
					require.Empty(t, value)
					return readErr
				},
				func(ctx context.Context, owner string) error {
					value, readErr := c.ExportPasses(ctx, owner)
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

func TestRegistrationBinaryHTTPCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		client := Client{
			Base:         "http://private-host.invalid",
			HTTP:         &http.Client{Transport: failureTransport{err: failure}},
			SandboxToken: (identity.Signer{}).Token,
		}
		_, err := client.DownloadPassProof(t.Context(), "alice", "dance", "alice")
		require.ErrorIs(t, err, failure)
		_, err = client.ExportPasses(t.Context(), "alice")
		require.ErrorIs(t, err, failure)
	}
}

func TestRegistrationBinaryRedirectIsolation(t *testing.T) {
	t.Parallel()
	var forwarded atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	client := Client{Base: redirect.URL, HTTP: redirect.Client(), SandboxToken: (identity.Signer{}).Token}
	_, err := client.UploadPassProof(t.Context(), "alice", "synthetic.txt", []byte("synthetic"))
	require.Error(t, err)
	_, err = client.DownloadPassProof(t.Context(), "alice", "dance", "alice")
	require.Error(t, err)
	_, err = client.ExportPasses(t.Context(), "alice")
	require.Error(t, err)
	host := Host{Base: redirect.URL, HTTP: redirect.Client(), UserToken: client.UserToken, Signer: identity.Signer{}}
	_, err = host.ExportPassSnapshot(t.Context(), "alice")
	require.Error(t, err)
	err = host.CheckPassExportSnapshot(t.Context(), "alice", []string{"dance"})
	require.Error(t, err)
	require.Zero(t, forwarded.Load())
}

func TestLocalRegistrationExportHostAuthorization(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{identity.ErrZitadelIdentity, context.Canceled, context.DeadlineExceeded, errors.New("private provider detail")} {
		calls := 0
		host := Host{
			UserToken: func(context.Context, string) (string, error) { calls++; return "token", nil },
			LocalDerived: &LocalDerived{
				Authorizer: applicationauth.Authorizer{
					DB:     permittedOrderOwner{},
					Verify: func(context.Context, string) (string, error) { return "alice", failure },
				},
			},
		}
		value, err := host.ExportPassSnapshot(t.Context(), "alice")
		require.Empty(t, value)
		assertClientFailure(t, err, failure)
		err = host.CheckPassExportSnapshot(t.Context(), "alice", []string{"dance"})
		assertClientFailure(t, err, failure)
		require.Equal(t, 2, calls)
	}
}
