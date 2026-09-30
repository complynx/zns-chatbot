package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type unavailableProvisioner struct {
	service     identityprovision.Service
	unavailable atomic.Bool
	calls       atomic.Int64
}

func (p *unavailableProvisioner) EnsureTelegram(
	ctx context.Context,
	input identityprovision.Telegram,
) (identityprovision.Binding, error) {
	p.calls.Add(1)
	if p.unavailable.Load() {
		return identityprovision.Binding{}, identityprovision.ErrUnavailable
	}
	return p.service.EnsureTelegram(ctx, input)
}

func onboardingInboxFixture(t *testing.T, language string) (*fixture, *unavailableProvisioner) {
	t.Helper()
	f := setup(t)
	links := identity.Links{DB: f.db, Issuer: "https://identity.invalid", BotID: 77}
	require.NoError(t, links.Bind(t.Context(), "alice", 101, "alice-provider"))
	require.NoError(t, links.Bind(t.Context(), "bob", 202, "bob-provider"))
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.zitadel_identities SET active=false WHERE owner='alice'; UPDATE core.users SET can_book=false WHERE id='alice'`,
	)
	require.NoError(t, err)
	service := identityprovision.Service{
		DB:           f.db,
		Provider:     &provisioningProvider{accounts: map[string]identityprovision.Account{}},
		Issuer:       links.Issuer,
		Organization: "test-org",
		BotID:        77,
		EmailDomain:  "telegram.invalid",
	}
	provider := &unavailableProvisioner{service: service}
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	server := httptest.NewServer(
		api.WithTelegramProvisioning(
			api.Handler(
				notificationFixtureServices(f.db, appservices.Options{}),
				signer,
				slog.New(slog.DiscardHandler),
			),
			provider,
			signer,
			77,
		),
	)
	t.Cleanup(server.Close)
	f.b.API = appclient.Client{
		Base:         server.URL,
		SandboxToken: signer.Token,
		Links:        links,
		Exchange:     provisioningExchange{links: links, signer: signer},
	}
	f.b.Host = appclient.Host{
		Base:      server.URL,
		Signer:    signer,
		UserToken: func(ctx context.Context, owner string) (string, error) { return f.b.API.UserToken(ctx, owner) },
	}
	f.b.Onboarding = func(ctx context.Context, user telegram.User) error { return f.b.Host.ProvisionTelegram(ctx, 77, user) }
	denied, healthy := message(9200, 101, "/orders"), message(9201, 202, "/orders")
	denied.Message.From.LanguageCode = language
	if language == "callback" {
		denied.Callback = &telegram.Callback{
			ID:      "denied",
			From:    denied.Message.From,
			Message: *denied.Message,
			Data:    "o:synthetic",
		}
		denied.Message = nil
	}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.telegram_inbox(update_id,payload) VALUES($1,$2),($3,$4)`,
		denied.ID,
		denied,
		healthy.ID,
		healthy,
	)
	require.NoError(t, err)
	return f, provider
}

// Wait for the real runtime worker and its receipts before asserting visible replies.
func completeIdentityInbox(t *testing.T, f *fixture, want int64) {
	t.Helper()
	runInboxUntil(t, f, func() bool {
		var cursor int64
		var pending, deliveries int
		err := f.db.QueryRow(t.Context(), `SELECT value,
 (SELECT count(*) FROM bot.telegram_inbox),
 (SELECT count(*) FROM bot.delivery_intents WHERE bot_id=$1
  AND (state IN ('pending','sending') OR (state='sent' AND NOT continuation_done)))
FROM bot.cursors WHERE name='telegram'`, f.b.Delivery.BotID).Scan(&cursor, &pending, &deliveries)
		return err == nil && cursor == want && pending == 0 && deliveries == 0
	})
}
func TestPermanentOnboardingDenialDoesNotBlockInbox(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru", "blocked", "callback"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, _ := onboardingInboxFixture(t, language)
			if language == "blocked" {
				post(t, f.fake.URL+"/lab/blocked", map[string]any{"user": 101, "blocked": true})
			}
			completeIdentityInbox(t, f, 9202)
			var active, canBook bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT z.active,u.can_book FROM core.zitadel_identities z JOIN core.users u ON u.id=z.owner WHERE u.id='alice'`).
					Scan(&active, &canBook),
			)
			require.False(t, active)
			require.False(t, canBook)
			var cards int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner='bob'`).Scan(&cards),
			)
			require.Positive(t, cards)
			if language != "blocked" {
				text, err := i18n.Translate(language, i18n.IdentityUnavailable, nil)
				require.NoError(t, err)
				require.True(
					t,
					slices.ContainsFunc(
						chatMessages(t, f, 101),
						func(m telegram.Message) bool { return m.Text == text },
					),
				)
			}
		})
	}
}

func TestTransientOnboardingFailureRemainsRetryable(t *testing.T) {
	t.Parallel()
	f, provider := onboardingInboxFixture(t, "en")
	provider.unavailable.Store(true)
	runInboxUntil(t, f, func() bool {
		var recorded bool
		err := f.db.QueryRow(t.Context(), `SELECT failures=1 AND state='pending' AND next_attempt_at>clock_timestamp()
 FROM bot.telegram_inbox WHERE update_id=9200`).Scan(&recorded)
		return err == nil && recorded
	})
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox
 WHERE update_id=9200 AND state='pending' AND failures=1`).Scan(&pending))
	require.Equal(t, 1, pending, "the original failed update must remain durable")
	provider.unavailable.Store(false)
	completeInboxAfterCooldown(t, f, 9202, 9200)
	require.GreaterOrEqual(t, provider.calls.Load(), int64(2))
}
