package bot

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestCreditCutoverConfigurationDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		active, enforce bool
		limit           int
		code, field     string
	}{
		{"legacy custom limit", false, false, 5, "", ""},
		{"legacy enforcing custom limit", false, true, 5, "", ""},
		{"enforcement required", true, false, 0, "credit_cutover_enforcement", "credits_enforce"},
		{"obsolete limit", true, true, 5, "credit_cutover_legacy_limit", "assistant_daily_limit"},
		{"default limit", true, true, config.DefaultAssistantDailyLimit, "", ""},
		{"unset limit", true, true, 0, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b := Bot{CreditsEnforce: test.enforce, AssistantDailyLimit: test.limit}
			err := b.creditCutoverConfiguration(test.active)
			if test.code == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var output bytes.Buffer
			logger := observability.NewLogger(&output, observability.LogConfig{})
			logger.ErrorContext(t.Context(), "cutover", "error", err)
			assert.Contains(t, output.String(), `"code":"`+test.code+`"`)
			assert.Contains(t, output.String(), `"field":"`+test.field+`"`)
			assert.Contains(t, output.String(), `"reason":"`+err.Error()+`"`)
			logger.ErrorContext(t.Context(), "unrelated", "error", errors.New("private-error-canary"))
			assert.NotContains(t, output.String(), "private-error-canary")
			assert.Contains(t, output.String(), `"error":"operation failed"`)
		})
	}
}
