package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestCombinedHistoryRevalidatesEachOperation(t *testing.T) {
	t.Parallel()
	calls := 0
	authorizer := applicationauth.Authorizer{
		Verify: func(context.Context, string) (string, error) { calls++; return "", context.Canceled },
	}
	client := combinedClient("http://127.0.0.1:1", nil, appservices.Services{}, authorizer)
	client.SandboxToken = (identity.Signer{}).Token
	require.NotNil(t, client.LocalHistory)
	generation, err := client.HistoryGeneration(t.Context(), "alice")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, generation)
	host := appclient.Host{UserToken: client.UserToken}
	configureLocalHost(&host, appservices.Services{}, authorizer)
	require.NotNil(t, host.LocalHistory)
	events, err := host.HistorySummaryBatch(t.Context(), "alice", 1)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, events)
	require.Equal(t, 2, calls)
}
