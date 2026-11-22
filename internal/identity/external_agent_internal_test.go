package identity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExternalAgentTokenBindsHumanAndSeparatesAudiences(t *testing.T) {
	t.Parallel()
	f, normal := fixture(t)
	options := normal.options
	options.ExternalAgentAudience = "mcp"
	options.ExternalAgentIntrospectionCredentials = ClientCredentials{ID: "mcp-intro", Secret: "mcp-intro-secret"}
	c, err := New(options, f.server.Client())
	require.NoError(t, err)
	f.users = []iamUser{testUser("human"), {ID: "machine", State: "USER_STATE_ACTIVE"}}
	f.introRoleClaims = map[string]any{
		"urn:zitadel:iam:org:project:api:roles": map[string]map[string]string{"zinusya.admin": {"org": "Org"}},
	}
	expiry := time.Now().Add(time.Minute)
	ctx := t.Context()
	for _, token := range []string{
		f.signed("human", "", "org", []string{"mcp", "api"}, expiry),
		f.signed("human", "", "org", []string{"mcp", "loom"}, expiry),
		f.signed("human", "actor", "org", []string{"mcp"}, expiry),
		f.signed("human", "", "other", []string{"mcp"}, expiry),
		f.signed("human", "", "org", []string{"api"}, expiry),
		f.signed("human", "", "org", []string{"mcp"}, time.Now().Add(-time.Minute)),
	} {
		_, err = c.ExternalAgentSubject(ctx, token)
		require.ErrorIs(t, err, ErrDenied)
	}
	machine, err := c.ExternalAgentSubject(ctx, f.signed("machine", "", "org", []string{"mcp"}, expiry))
	require.NoError(t, err)
	_, err = c.IssueExternalAgentToken(ctx, machine)
	require.ErrorIs(t, err, ErrDenied)

	token := f.signed("human", "", "org", []string{"mcp"}, expiry)
	_, err = c.Verify(ctx, token)
	require.ErrorIs(t, err, ErrDenied)
	subject, err := c.ExternalAgentSubject(ctx, token)
	require.NoError(t, err)
	credential, err := c.IssueExternalAgentToken(ctx, subject)
	require.NoError(t, err)
	require.NotEqual(t, token, credential.Token)
	require.Equal(t, subject.ExpiresAt, credential.ExpiresAt)
	agent, err := c.Verify(ctx, credential.Token)
	require.NoError(t, err)
	require.Equal(t, "human", agent.Subject)
	require.True(t, agent.ExternalAgent)
	require.Contains(t, agent.Roles["zinusya.admin"], "org")

	internal, err := c.UserToken(ctx, "human")
	require.NoError(t, err)
	person, err := c.Verify(ctx, internal.Token)
	require.NoError(t, err)
	require.False(t, person.ExternalAgent)

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, form := range f.forms {
		if len(form["audience"]) == 1 {
			require.Equal(t, []string{"api"}, form["audience"])
			require.NotContains(t, form["scope"][0], "project:id:loom:aud")
		}
	}
}
