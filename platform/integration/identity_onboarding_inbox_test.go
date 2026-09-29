package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
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
			api.Handler(runtimeapp.NewServices(f.db, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler)),
			provider,
			signer,
			77,
		),
	)
	t.Cleanup(server.Close)
	f.b.API = bot.APIClient{
		Base:     server.URL,
		Signer:   signer,
		Links:    links,
		Exchange: provisioningExchange{links: links, signer: signer},
	}
	f.b.Onboarding = func(ctx context.Context, user telegram.User) error { return f.b.API.ProvisionTelegram(ctx, 77, user) }
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

func TestPermanentOnboardingDenialDoesNotBlockInbox(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru", "blocked", "callback"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, _ := onboardingInboxFixture(t, language)
			if language == "blocked" {
				post(t, f.fake.URL+"/lab/blocked", map[string]any{"user": 101, "blocked": true})
			}
			completeInbox(t, f, 9202)
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
	runInboxUntil(t, f, func() bool { return provider.calls.Load() > 0 })
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Equal(t, 2, pending)
	provider.unavailable.Store(false)
	completeInbox(t, f, 9202)
	require.GreaterOrEqual(t, provider.calls.Load(), int64(2))
}
