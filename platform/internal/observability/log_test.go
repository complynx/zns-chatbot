package observability_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestLogPrivacyAndGroups(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := observability.NewLogger(
		&output,
		observability.LogConfig{Level: observability.LevelTrace, Secrets: []string{"configured-secret"}},
	)
	bound := logger.With("password", "hidden").WithGroup("worker").With("email", "person@example.test")
	bound.InfoContext(
		t.Context(),
		"request configured-secret",
		slog.Group("nested", "api_key", "hidden-key", "count", 2),
		"error",
		errors.New("https://user:password@example.test/?key=private"),
		"url",
		"http://user:pass@example.test/?private=yes",
		"jwt",
		"eyJheader.eyJpayload.signature",
		"object",
		map[string]any{"phone": "12345", "safe": "configured-secret"},
	)
	text := output.String()
	for _, secret := range []string{"configured-secret", "person@example.test", "hidden", "12345", "eyJheader", "user:password", "private=yes"} {
		assert.NotContains(t, text, secret)
	}
	assert.Contains(t, text, `"worker":{"email":"[redacted]"`)
	assert.Contains(t, text, `"count":2`)
	assert.Contains(t, text, `"error":"operation failed"`)
	output.Reset()
	bound.DebugContext(
		t.Context(),
		"debug",
		"jwt",
		"eyJheader.eyJpayload.signature",
		"text",
		"hello",
		"url",
		"https://user:pass@example.test/?key=value",
	)
	assert.Contains(t, output.String(), "person@example.test")
	assert.Contains(t, output.String(), "eyJheader.eyJpayload.[redacted]")
	assert.NotContains(t, output.String(), "signature")
	assert.NotContains(t, output.String(), "user:pass")
	output.Reset()
	bound.Log(t.Context(), observability.LevelTrace, "trace", "text", "private trace content")
	assert.Contains(t, output.String(), `"level":"TRACE"`)
	assert.NotContains(t, output.String(), "private trace content")
}

func TestLogBoundsAndLevels(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := observability.NewLogger(&output, observability.LogConfig{Level: slog.LevelWarn})
	logger.InfoContext(t.Context(), "ignored")
	assert.Empty(t, output.String())
	cyclic := map[string]any{}
	cyclic["child"] = cyclic
	logger.WarnContext(t.Context(), strings.Repeat("a", 10000), "nested", cyclic)
	require.Less(t, output.Len(), 6000)
	assert.Contains(t, output.String(), `"level":"WARNING"`)
	assert.Contains(t, output.String(), "[truncated]")
}
