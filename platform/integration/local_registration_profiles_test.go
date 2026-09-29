package integration_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestLocalRegistrationProfilesHTTPParity(t *testing.T) {
	t.Parallel()
	db := database(t)
	profileService := passes.Service{DB: db}
	signer := identity.Signer{Key: []byte("registration-profiles-test-key-32")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.Services{Core: core.Service{DB: db}, PassProfiles: profileService},
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{
		SandboxToken: signer.Token,
		LocalRegistration: &appclient.LocalRegistration{
			Profile:    profileService,
			Authorizer: applicationauth.Authorizer{DB: db, Verify: verify},
		},
	}
	initial, err := local.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	remoteInitial, err := remote.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, remoteInitial, initial)
	first := profileCommand("set", "legal_name", "local-profile-first", initial)
	first.Value = "Synthetic Name First"
	current, err := local.ExecutePassProfile(t.Context(), "alice", first)
	require.NoError(t, err)
	initialReplay, err := remote.ExecutePassProfile(t.Context(), "alice", first)
	require.NoError(t, err)
	require.Equal(t, current, initialReplay)
	for n := range 21 {
		command := profileCommand("set", "legal_name", fmt.Sprintf("local-profile-%d", n), current)
		command.Value = fmt.Sprintf("Synthetic Name %d", n)
		current, err = local.ExecutePassProfile(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	for _, client := range []appclient.Client{local, remote} {
		latestReplay, readErr := client.ExecutePassProfile(t.Context(), "alice", first)
		require.NoError(t, readErr)
		require.Equal(t, current, latestReplay)
		require.NotEqual(t, first.Value, latestReplay.LegalName)
		bob, readErr := client.PassProfile(t.Context(), "bob")
		require.NoError(t, readErr)
		require.Empty(t, bob.LegalName)
		history, readErr := client.PassProfileHistory(t.Context(), "bob")
		require.NoError(t, readErr)
		require.Empty(t, history)
		_, readErr = client.PassProfileHistoryPage(t.Context(), "alice", -1)
		requireCode(t, readErr, "invalid_cursor")
		invalid := profileCommand("set", "legal_name", "too-long", current)
		invalid.Value = strings.Repeat("x", 301)
		_, readErr = client.ExecutePassProfile(t.Context(), "alice", invalid)
		requireCode(t, readErr, "pass_profile_invalid")
		_, readErr = client.PassProfile(t.Context(), "missing-owner")
		requireCode(t, readErr, "forbidden")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.PassProfile(ctx, "alice")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassProfileHistory(ctx, "alice")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassProfileHistoryPage(ctx, "alice", 0)
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.ExecutePassProfile(ctx, "alice", first)
		require.ErrorIs(t, readErr, context.Canceled)
	}
	history, err := local.PassProfileHistory(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, history, 22)
	remoteHistory, err := remote.PassProfileHistory(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, remoteHistory, history)
	before := int64(0)
	for _, count := range []int{20, 2} {
		page, readErr := local.PassProfileHistoryPage(t.Context(), "alice", before)
		require.NoError(t, readErr)
		require.Len(t, page.Items, count)
		other, readErr := remote.PassProfileHistoryPage(t.Context(), "alice", before)
		require.NoError(t, readErr)
		require.Equal(t, other, page)
		before = page.NextBefore
	}
	require.Zero(t, before)
	_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.ExecutePassProfile(t.Context(), "alice", first)
		requireCode(t, err, "forbidden")
	}
	var receipts int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
			Scan(&receipts),
	)
	require.Equal(t, 22, receipts)
}
