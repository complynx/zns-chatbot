package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestCombinedKnowledgeRevalidatesEachOperation(t *testing.T) {
	t.Parallel()
	calls := 0
	failure := identity.ErrZitadelIdentity
	c := combinedClient("http://127.0.0.1:1", nil, appservices.Services{}, applicationauth.Authorizer{
		Verify: func(context.Context, string) (string, error) { calls++; return "", failure },
	})
	require.NotNil(t, c.LocalKnowledge)
	c.SandboxToken = (identity.Signer{}).Token
	_, err := c.Knowledge(t.Context(), "alice", knowledge.Query{})
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusUnauthorized, problem.Status)
	failure = context.Canceled
	_, err = c.ExecuteKnowledge(t.Context(), "alice", knowledge.Command{})
	require.ErrorIs(t, err, context.Canceled)
	host := appclient.Host{UserToken: c.UserToken}
	configureLocalHost(&host, appservices.Services{}, c.LocalKnowledge.Authorizer)
	require.NotNil(t, host.LocalKnowledge)
	require.NotNil(t, host.LocalDerived)
	_, err = host.ExecuteDerivedKnowledge(t.Context(), "alice", knowledge.Command{}, readsource.Derivation{})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 3, calls)
}
