//nolint:testpackage // Tests exercise expiry and concurrency on the private token cache.
package identity

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func machineFixture(t *testing.T) (*iamFixture, *MachineTokenSource) {
	t.Helper()
	f, _ := fixture(t)
	f.machineScopeOnly = true
	source, err := NewMachineTokenSource(MachineOptions{
		Issuer: f.server.URL, OrganizationID: "org", Audience: "loom", Subject: "actor",
		Credentials: ClientCredentials{ID: "machine", Secret: "machine-secret"},
	}, f.server.Client())
	require.NoError(t, err)
	return f, source
}

func TestMachineTokenSharedRefresh(t *testing.T) {
	t.Parallel()
	f, source := machineFixture(t)
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			_, err := source.Token(t.Context(), 6*time.Minute)
			assertMachineToken(t, err)
		})
	}
	workers.Wait()
	f.mu.Lock()
	require.Equal(t, 1, f.tokens)
	f.mu.Unlock()
	source.mu.Lock()
	source.credential.ExpiresAt = time.Now().Add(5 * time.Minute)
	source.mu.Unlock()
	_, err := source.Token(t.Context(), 6*time.Minute)
	require.NoError(t, err)
	f.mu.Lock()
	require.Equal(t, 2, f.tokens)
	f.mu.Unlock()
}

func assertMachineToken(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}

func TestMachineTokenRejectsWrongIdentityAndShortLifetime(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"subject", "organization", "lifetime"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			_, source := machineFixture(t)
			minimum := 6 * time.Minute
			switch kind {
			case "subject":
				source.options.Subject = "some-human"
			case "organization":
				source.client.options.OrganizationID = "other-org"
			case "lifetime":
				minimum = 2 * time.Hour
			}
			_, err := source.Token(t.Context(), minimum)
			require.ErrorIs(t, err, ErrDenied)
			require.Empty(t, source.credential.Token)
		})
	}
}

func TestMachineTokenFailureDoesNotExposeCredentials(t *testing.T) {
	t.Parallel()
	f, source := machineFixture(t)
	f.tokenStatus = http.StatusServiceUnavailable
	_, err := source.Token(t.Context(), 6*time.Minute)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotContains(t, err.Error(), "secret")
	f.mu.Lock()
	f.tokenStatus = 0
	f.mu.Unlock()
	_, err = source.Token(t.Context(), 6*time.Minute)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = source.Token(ctx, 6*time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}
