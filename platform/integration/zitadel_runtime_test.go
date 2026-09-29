package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// Runtime boundary tests use a deterministic provider; the adapter's TLS/live
// tests separately verify the actual Zitadel OAuth wire contract and claims.
type runtimeProvider struct{}

func (runtimeProvider) Exchange(_ context.Context, subject string) (string, error) {
	return "delegated:" + subject, nil
}

func (runtimeProvider) Verify(_ context.Context, token string) (string, error) {
	subject, ok := strings.CutPrefix(token, "delegated:")
	if !ok || subject == "" {
		return "", identity.ErrZitadelIdentity
	}
	return subject, nil
}

func TestZitadelRuntimeUsesLinkedOwnerAndExistingACL(t *testing.T) {
	t.Parallel()
	db := database(t)
	links := identity.Links{DB: db, Issuer: "https://identity.invalid", BotID: 123}
	require.NoError(t, links.Bind(t.Context(), "alice", 101, "z-alice"))
	require.NoError(t, links.Bind(t.Context(), "bob", 202, "z-bob"))
	signer := identity.Signer{Key: []byte(strings.Repeat("s", identity.MinKeyBytes))}
	server := httptest.NewServer(api.AuthenticatedHandler(appservices.NewServices(db, appservices.Options{}), signer,
		slog.New(slog.DiscardHandler), api.ZitadelOwner(runtimeProvider{}, links)))
	t.Cleanup(server.Close)
	client := appclient.Client{Base: server.URL, SandboxToken: signer.Token, Exchange: runtimeProvider{}, Links: links}
	ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	created, err := client.ExecuteOrder(ctx, owner, orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "identity-order",
		Choice:  orderChoice("shuttle"),
	})
	require.NoError(t, err)
	for _, origin := range []string{"manual", "agent"} {
		current, readErr := client.Current(ctx, owner)
		require.NoError(t, readErr)
		_, err = client.Execute(ctx, owner, action("select", "shuttle-1", current.Version, "identity-"+origin, origin))
		require.NoError(t, err)
	}
	gateway := (miniapp.Gateway{API: client, Token: "test-token", EventID: "sandbox-festival"}).Handler()
	path := "/miniapp/api/orders/" + created.ID
	assert.Equal(t, http.StatusOK, webRequest(t, gateway, http.MethodGet, path, 101, nil).Code)
	assert.Equal(t, http.StatusNotFound, webRequest(t, gateway, http.MethodGet, path, 202, nil).Code)
	assert.Equal(t, http.StatusForbidden, webRequest(t, gateway, http.MethodGet, path, 303, nil).Code)
	_, err = (appclient.Client{Base: server.URL, SandboxToken: signer.Token}).Current(t.Context(), "alice")
	require.Error(t, err, "Zitadel API must reject synthetic bearer")
	_, err = (appclient.Host{Base: server.URL, Signer: signer}).PendingNotifications(t.Context())
	require.NoError(t, err, "service notification auth remains independent")
	_, err = db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=false WHERE owner='alice'`)
	require.NoError(t, err)
	_, err = client.Current(ctx, owner)
	require.Error(t, err, "already resolved context cannot bypass revoked link")
	assert.Equal(t, http.StatusForbidden, webRequest(t, gateway, http.MethodGet, path, 101, nil).Code)
}
