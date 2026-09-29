package mediaproc

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestBrokerRequiresAuthenticatedAccountingModeAgreement(t *testing.T) {
	t.Parallel()
	broker, err := NewBroker(
		BrokerConfig{Secret: "synthetic", SocketPath: filepath.Join(t.TempDir(), "decoder.sock"), CreditsEnforce: true},
	)
	require.NoError(t, err)
	for _, enforce := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, preprocessPath, nil)
		request.Header.Set("Authorization", "Bearer synthetic")
		credits.SetRequestMode(request, enforce)
		response := httptest.NewRecorder()
		broker.ServeHTTP(response, request)
		if enforce {
			require.Equal(
				t,
				http.StatusRequestEntityTooLarge,
				response.Code,
				"mode agrees even when no paid recognizer is configured",
			)
		} else {
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Contains(t, response.Body.String(), "accounting mode mismatch")
		}
	}
}
