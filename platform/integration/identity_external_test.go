package integration_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

type externalProvider struct {
	mu       sync.Mutex
	links    map[string][]identityprovision.ExternalIdentity
	adds     int
	failRead bool
	failed   bool
}

func (p *externalProvider) Links(_ context.Context, subject string) ([]identityprovision.ExternalIdentity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failRead && p.adds > 0 && !p.failed {
		p.failed = true
		return nil, identityprovision.ErrUnavailable
	}
	return append([]identityprovision.ExternalIdentity(nil), p.links[subject]...), nil
}

func (p *externalProvider) AddLink(_ context.Context, subject string, link identityprovision.ExternalIdentity) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.adds++
	p.links[subject] = append(p.links[subject], link)
	return identityprovision.ErrUnavailable // Remote commit followed by a lost response.
}

func externalFixture(t *testing.T) (identityprovision.ExternalLinker, *externalProvider) {
	t.Helper()
	provider := &externalProvider{links: map[string][]identityprovision.ExternalIdentity{}}
	service := identityprovision.Service{
		DB:           database(t),
		Provider:     &provisioningProvider{accounts: map[string]identityprovision.Account{}},
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        77,
		EmailDomain:  "telegram.invalid",
	}
	return identityprovision.ExternalLinker{Provisioner: service, Provider: provider, IDP: "trusted-relay"}, provider
}

func TestExternalIdentityConcurrentReplayAndConflict(t *testing.T) {
	t.Parallel()
	linker, provider := externalFixture(t)
	input := identityprovision.Telegram{ID: 95201, FirstName: "Synthetic"}
	const subject = "telegram:77:opaque-oidc-sub"
	errors := make(chan error, 4)
	for range 4 {
		go func() { _, err := linker.EnsureExternal(t.Context(), input, subject); errors <- err }()
	}
	for range 4 {
		require.NoError(t, <-errors)
	}
	require.Equal(t, 1, provider.adds)
	binding, err := linker.EnsureExternal(t.Context(), input, subject)
	require.NoError(t, err)
	var ready bool
	require.NoError(
		t,
		linker.Provisioner.DB.QueryRow(t.Context(), `SELECT ready FROM core.identity_external_links WHERE owner=$1`, binding.Owner).
			Scan(&ready),
	)
	require.True(t, ready)
	_, err = linker.EnsureExternal(t.Context(), input, "telegram:77:another-sub")
	require.ErrorIs(t, err, identityprovision.ErrConflict)
	input.ID++
	_, err = linker.EnsureExternal(t.Context(), input, subject)
	require.ErrorIs(t, err, identityprovision.ErrConflict)
	require.Equal(t, 1, provider.adds)
}

func TestExternalIdentityResumesProviderCommitBeforeReadback(t *testing.T) {
	t.Parallel()
	linker, provider := externalFixture(t)
	provider.failRead = true
	input := identityprovision.Telegram{ID: 95202, FirstName: "Synthetic"}
	_, err := linker.EnsureExternal(t.Context(), input, "telegram:77:trusted-sub")
	require.ErrorIs(t, err, identityprovision.ErrUnavailable)
	var ready bool
	require.NoError(
		t,
		linker.Provisioner.DB.QueryRow(t.Context(), `SELECT ready FROM core.identity_external_links WHERE telegram_id=$1`, input.ID).
			Scan(&ready),
	)
	require.False(t, ready)
	binding, err := linker.EnsureExternal(t.Context(), input, "telegram:77:trusted-sub")
	require.NoError(t, err)
	require.Equal(t, 1, provider.adds)
	provider.links[binding.Subject] = nil
	_, err = linker.EnsureExternal(t.Context(), input, "telegram:77:trusted-sub")
	require.ErrorIs(t, err, identityprovision.ErrConflict, "an intentionally removed ready link must not be recreated")
	require.Equal(t, 1, provider.adds)
}

func TestExternalIdentityDoesNotReplaceProviderConflict(t *testing.T) {
	t.Parallel()
	linker, provider := externalFixture(t)
	input := identityprovision.Telegram{ID: 95203, FirstName: "Synthetic"}
	binding, err := linker.Provisioner.EnsureTelegram(t.Context(), input)
	require.NoError(t, err)
	provider.links[binding.Subject] = []identityprovision.ExternalIdentity{
		{IDP: linker.IDP, Subject: "telegram:77:other"},
	}
	_, err = linker.EnsureExternal(t.Context(), input, "telegram:77:trusted-sub")
	require.ErrorIs(t, err, identityprovision.ErrConflict)
	require.Zero(t, provider.adds)
}
