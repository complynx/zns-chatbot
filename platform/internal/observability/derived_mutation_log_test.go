package observability_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestDerivedMutationCredentialRedaction(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	credential := signer.DerivedMutationToken("alice")
	for _, level := range []slog.Level{slog.LevelDebug, observability.LevelTrace} {
		var output bytes.Buffer
		logger := observability.NewLogger(&output, observability.LogConfig{Level: observability.LevelTrace})
		logger.Log(t.Context(), level, "host request", "X-ZNS-Derivation", credential)
		logger.With("x-zns-derivation", credential).Log(t.Context(), level, "bound host request")
		logger.Log(t.Context(), level, "grouped host request", slog.Group("headers", "X-Zns-Derivation", credential))
		require.NotContains(t, output.String(), credential)
		require.Contains(t, output.String(), "[redacted]")
	}
}
