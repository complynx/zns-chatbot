package appclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type failingIdentity struct{ err error }

func (f failingIdentity) Telegram(context.Context, int64) (identity.User, error) {
	return identity.User{}, f.err
}
func (failingIdentity) Exchange(context.Context, string) (string, error) { return "", nil }

type failingResponse struct{ err error }

func (f failingResponse) Read([]byte) (int, error) { return 0, f.err }
func (failingResponse) Close() error               { return nil }

type failureTransport struct {
	err    error
	status int
}

func (f failureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if f.status == 0 {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: failingResponse{err: f.err}, Header: make(http.Header)}, nil
}

func TestClientCancellationAndSanitization(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("private-url-and-password")} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			wrapped := fmt.Errorf("private-url-and-password: %w", failure)
			link := failingIdentity{err: wrapped}
			client := Client{Links: link, Exchange: link, SandboxToken: (identity.Signer{}).
				Token,
			}
			_, owner, err := client.AuthenticateTelegram(t.Context(), 101)
			require.Empty(t, owner)
			assertClientFailure(t, err, failure)
			for _, status := range []int{0, http.StatusOK, http.StatusServiceUnavailable} {
				client = Client{
					Base: "http://private-host.invalid",
					HTTP: &http.Client{
						Transport: failureTransport{err: wrapped, status: status},
					},
					SandboxToken: (identity.Signer{}).
						Token,
				}
				err = requestToken(t.Context(), client.Base, client.HTTP,
					"secret-token",
					http.MethodGet,
					"/v1/orders",
					nil,
					new(any),
				)
				assertClientFailure(t, err, failure)
			}
		})
	}
}

func assertClientFailure(t *testing.T, err, expected error) {
	t.Helper()
	require.Error(t, err)
	if errors.Is(expected, context.Canceled) || errors.Is(expected, context.DeadlineExceeded) {
		require.ErrorIs(t, err, expected)
	}
	require.NotContains(t, err.Error(), "private")
	require.NotContains(t, err.Error(), "secret")
}

func TestCanceledCallerSurvivesSanitizedTransportError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := Client{Base: "http://127.0.0.1:1", SandboxToken: (identity.Signer{}).
		Token,
	}
	err := requestToken(ctx, client.Base, client.HTTP, "token", http.MethodGet, "/v1/orders", nil, new(any))
	require.ErrorIs(t, err, context.Canceled)
	link := failingIdentity{err: errors.New("driver cancellation text")}
	client = Client{Links: link, Exchange: link, SandboxToken: (identity.Signer{}).
		Token,
	}
	_, _, err = client.AuthenticateTelegram(ctx, 101)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIdentityLookupPreservesDatabaseClassification(t *testing.T) {
	t.Parallel()
	link := failingIdentity{err: core.DatabaseFailure(errors.New("private SQL password"))}
	client := Client{Links: link, Exchange: link}
	_, owner, err := client.AuthenticateTelegram(t.Context(), 101)
	require.Empty(t, owner)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, "identity lookup unavailable")
	require.NotContains(t, err.Error(), "private")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = client.AuthenticateTelegram(ctx, 101)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, "identity lookup unavailable")
	require.NotContains(t, err.Error(), "private")
	link.err = identity.ErrZitadelUnavailable
	client.Links = link
	_, _, err = client.AuthenticateTelegram(t.Context(), 101)
	require.False(t, core.IsDatabaseFailure(err))
	require.EqualError(t, err, "identity lookup unavailable")
}
