package appclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
)

func TestReadPassOperationDiscardsPartialHTTPResult(t *testing.T) {
	t.Parallel()
	for name, response := range map[string]string{
		"malformed": `{"summary":{"status":"committed"},"read_authorities":"invalid"}`,
		"oversized": `{"summary":{"status":"` + strings.Repeat("x", MaxAPIBytes) + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(response)) }),
			)
			t.Cleanup(server.Close)
			host := Host{
				Base:      server.URL,
				HTTP:      server.Client(),
				UserToken: func(context.Context, string) (string, error) { return "synthetic", nil },
			}
			value, err := host.ReadPassOperation(t.Context(), "alice", derivedmutation.PassOperationInput{Export: true})
			require.Error(t, err)
			require.Equal(t, derivedmutation.PassOperationRead{}, value)
		})
	}
}

func TestReadPassOperationFreshIdentity(t *testing.T) {
	t.Parallel()
	calls := 0
	host := Host{UserToken: func(context.Context, string) (string, error) { calls++; return "foreign", nil },
		LocalDerived: &LocalDerived{Authorizer: applicationauth.Authorizer{DB: permittedOrderOwner{},
			Verify: func(context.Context, string) (string, error) { return "bob", nil }}}}
	input := derivedmutation.PassOperationInput{Export: true}
	for range 2 {
		value, err := host.ReadPassOperation(t.Context(), "alice", input)
		var problem *core.ProblemError
		require.ErrorAs(t, err, &problem)
		require.Equal(t, "unauthorized", problem.Code)
		require.Equal(t, derivedmutation.PassOperationRead{}, value)
	}
	require.Equal(t, 2, calls)
	_, err := host.ReadPassOperation(t.Context(), "alice", derivedmutation.PassOperationInput{})
	require.Error(t, err)
	require.Equal(t, 2, calls)
	providerError := errors.New("synthetic identity outage")
	host.UserToken = func(context.Context, string) (string, error) { return "", providerError }
	_, err = host.ReadPassOperation(t.Context(), "alice", input)
	require.ErrorIs(t, err, providerError)
	host.LocalDerived = nil
	_, err = host.ReadPassOperation(t.Context(), "alice", input)
	require.ErrorIs(t, err, providerError)
}
