package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type reconciliationProvider struct {
	mu       sync.Mutex
	subjects map[string]int
}

func (p *reconciliationProvider) Exchange(ctx context.Context, subject string) (string, error) {
	p.mu.Lock()
	p.subjects[subject]++
	p.mu.Unlock()
	return (runtimeProvider{}).Exchange(ctx, subject)
}

func (p *reconciliationProvider) count(subject string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.subjects[subject]
}

func runReconciliation(t *testing.T, b *bot.Bot) func() {
	t.Helper()
	running, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- b.Run(running) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); require.NoError(t, <-finished) }) }
	t.Cleanup(stop)
	return stop
}

func TestZitadelBackgroundReconcilesSeparateRecipients(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	for owner, chat := range map[string]int64{"alice": 101, "bob": 202} {
		require.NoError(t, f.b.Render(ctx, owner, chat))
		require.NoError(t, f.b.RenderProfile(ctx, owner, chat))
	}
	links := identity.Links{DB: f.db, Issuer: "https://identity.invalid", BotID: 123}
	require.NoError(t, links.Bind(ctx, "alice", 101, "z-alice"))
	require.NoError(t, links.Bind(ctx, "bob", 202, "z-bob"))
	provider := &reconciliationProvider{subjects: map[string]int{}}
	server := httptest.NewServer(
		api.AuthenticatedHandler(runtimeapp.NewServices(f.db, runtimeapp.Options{}), f.b.API.Signer,
			slog.New(slog.DiscardHandler), api.ZitadelOwner(runtimeProvider{}, links)),
	)
	t.Cleanup(server.Close)
	f.b.API.Base, f.b.API.Links, f.b.API.Exchange = server.URL, links, provider
	f.b.Logger = slog.New(slog.DiscardHandler)
	_, err := f.db.Exec(
		ctx,
		`UPDATE bot.messages SET view_hash='refresh'; UPDATE bot.order_cards SET view_hash='refresh'`,
	)
	require.NoError(t, err)
	stop := runReconciliation(t, f.b)
	awaitReconciled := func(owner string) {
		t.Helper()
		require.Eventually(t, func() bool {
			var count int
			queryErr := f.db.QueryRow(ctx, `SELECT count(*) FROM (
SELECT view_hash FROM bot.messages WHERE owner=$1 UNION ALL
SELECT view_hash FROM bot.order_cards WHERE owner=$1 AND card_key='profile') v WHERE view_hash='refresh'`, owner).Scan(&count)
			return queryErr == nil && count == 0
		}, 5*time.Second, 20*time.Millisecond)
	}
	awaitReconciled("alice")
	awaitReconciled("bob")
	assert.Positive(t, provider.count("z-alice"))
	assert.Positive(t, provider.count("z-bob"))
	stop()
	// Revoke Bob while Alice continues refreshing. The durable card stays pending;
	// restoring the binding permits the next polling cycle to refresh it.
	_, err = f.db.Exec(ctx, `UPDATE core.zitadel_identities SET active=false WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = f.db.Exec(
		ctx,
		`UPDATE bot.messages SET view_hash='refresh'; UPDATE bot.order_cards SET view_hash='refresh'`,
	)
	require.NoError(t, err)
	before := provider.count("z-bob")
	runReconciliation(t, f.b)
	awaitReconciled("alice")
	assert.Equal(t, before, provider.count("z-bob"))
	var hash string
	require.NoError(t, f.db.QueryRow(ctx, `SELECT view_hash FROM bot.messages WHERE owner='bob'`).Scan(&hash))
	assert.Equal(t, "refresh", hash)
	_, err = f.db.Exec(ctx, `UPDATE core.zitadel_identities SET active=true WHERE owner='bob'`)
	require.NoError(t, err)
	awaitReconciled("bob")
	assert.Greater(t, provider.count("z-bob"), before)
}
