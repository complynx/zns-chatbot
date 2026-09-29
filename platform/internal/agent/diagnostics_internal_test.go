package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestStructuredDiagnosticsFollowAuthorization(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	ctx := observability.WithAgentEvents(t.Context(), recorder)
	authorized := 0
	input := Input{
		Text:           "private-canary",
		BeforeProvider: func(context.Context, *Input) error { authorized++; return nil },
	}
	call := authorizedStructuredCall(&input, func(_ context.Context, prompt providerPrompt) (string, error) {
		require.Positive(t, authorized)
		require.Contains(t, string(prompt.input), "private-canary")
		return "private-result-canary", nil
	})
	_, err = call(ctx, providerPrompt{name: selectionName})
	require.NoError(t, err)
	_, err = call(ctx, providerPrompt{name: "zns_action_plan"})
	require.NoError(t, err)
	require.Equal(t, 2, authorized)
	require.NotContains(t, log.String(), "canary")
	require.Equal(t, 4, strings.Count(log.String(), "agent diagnostic"))
	require.Contains(t, log.String(), "model.skills")
	require.Contains(t, log.String(), "model.plan")
	recordPlanValidation(ctx, errors.New("private-validation-canary"))
	require.Contains(t, log.String(), "model.validation")
	require.NotContains(t, log.String(), "canary")
}
