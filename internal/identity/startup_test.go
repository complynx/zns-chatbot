//nolint:testpackage // Reuse the signed local IAM fixture and inspect requests for side effects.
package identity

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartupAccessUsesOnlyServiceTokenAndIntrospection(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	credential, err := c.CheckAccess(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, credential.Token)
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Equal(t, 1, f.tokens)
	require.Equal(t, 1, f.introspections)
	require.Zero(t, f.creates)
	require.Equal(t, "client_credentials", f.forms[0][grantTypeKey][0])
}

func TestStartupAccessRejectsBadCredentialsWithoutLeaking(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"service", "introspection"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			f, c := fixture(t)
			if stage == "service" {
				f.tokenStatus = http.StatusUnauthorized
			} else {
				f.introStatus = http.StatusUnauthorized
			}
			_, err := c.CheckAccess(t.Context())
			require.ErrorContains(t, err, "IAM "+stage+" credentials")
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
