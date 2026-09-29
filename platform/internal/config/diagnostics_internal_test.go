package config

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestDiagnosticsKeepArbitraryErrorsRedacted(t *testing.T) {
	t.Parallel()
	const canary = "PRIVATE-PROVIDER-RESPONSE-CANARY"
	for _, err := range []error{
		errors.New(canary),
		fmt.Errorf("%s: %w", canary, errDiagnosticSigningSecret),
	} {
		var output bytes.Buffer
		observability.NewLogger(&output, observability.LogConfig{}).ErrorContext(t.Context(), "stopped", "error", err)
		assert.Contains(t, output.String(), `"error":"operation failed"`)
		assert.NotContains(t, output.String(), canary)
	}
}

func TestDiagnosticsClosedCatalog(t *testing.T) {
	t.Parallel()
	for code := errDiagnosticUnknown; code <= errDiagnosticBotNamespace; code++ {
		info := code.info()
		require.NotEmpty(t, info.code)
		require.NotEmpty(t, info.field)
		require.NotEmpty(t, info.reason)
		assert.Equal(t, info.reason, code.Error())
	}
	const invalid diagnostic = 255
	assert.Equal(t, errDiagnosticUnknown.info(), invalid.info())
}
