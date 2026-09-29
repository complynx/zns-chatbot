package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type inboxOAuth struct {
	mode   atomic.Int32
	calls  atomic.Int64
	issuer string
}

func (p *inboxOAuth) serve(w http.ResponseWriter, r *http.Request) {
	if r.ParseForm() != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.URL.Path == "/oauth/v2/introspect" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true, "sub": strings.TrimPrefix(r.Form.Get("token"), "delegated:"),
			"iss": p.issuer, "aud": []string{"project"}, "client_id": "bot-client",
			"exp": time.Now().Add(time.Hour).Unix(), "act": map[string]string{"sub": "machine"},
		})
		return
	}
	subject := r.Form.Get("subject_token")
	if subject == "z-alice" {
		calls := p.calls.Add(1)
		if p.mode.Load() == 5 && calls > 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"Errors.User.NotActive"}`))
			return
		}
		switch p.mode.Load() {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"Errors.User.NotActive"}`))
			return
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case 3:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		case 4:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"Errors.PermissionDenied"}`))
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "delegated:" + subject, "token_type": "Bearer",
		"issued_token_type": "urn:ietf:params:oauth:token-type:jwt", "expires_in": 60,
	})
}

func providerInboxFixture(t *testing.T, mode int32, language string) (*fixture, *inboxOAuth, *atomic.Int64) {
	t.Helper()
	f := setup(t)
	service := workflow.Service{DB: f.db}
	selected := mustExec(t, service, "alice", action("select", "massage-1", 0, "provider-select", "manual"))
	mustExec(t, service, "alice", action("confirm", "", selected.Version, "provider-confirm", "manual"))
	provider := &inboxOAuth{}
	provider.mode.Store(mode)
	oauth := httptest.NewTLSServer(http.HandlerFunc(provider.serve))
	t.Cleanup(oauth.Close)
	provider.issuer = oauth.URL
	adapter, err := identity.NewZitadel(identity.ZitadelConfig{
		Issuer:          oauth.URL,
		Audience:        "project",
		BotClientID:     "bot-client",
		BotClientSecret: "bot-secret",
		APIClientID:     "api-client",
		APIClientSecret: "api-secret",
		ActorID:         "machine",
		ActorToken:      "machine-token",
		HTTP:            oauth.Client(),
	})
	require.NoError(t, err)
	links := identity.Links{DB: f.db, Issuer: oauth.URL, BotID: 77}
	require.NoError(t, links.Bind(t.Context(), "alice", 101, "z-alice"))
	require.NoError(t, links.Bind(t.Context(), "bob", 202, "z-bob"))
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.NewServices(f.db, appservices.Options{}),
			f.b.Host.Signer,
			slog.New(slog.DiscardHandler),
			api.ZitadelOwner(adapter, links),
		),
	)
	t.Cleanup(server.Close)
	f.b.API.Base, f.b.API.Links, f.b.API.Exchange = server.URL, links, adapter
	f.b.Host.Base = f.b.API.Base
	f.b.Logger = slog.New(slog.DiscardHandler)
	f.b.Onboarding = func(context.Context, telegram.User) error {
		t.Error("mapped provider rejection must not provision")
		return nil
	}
	acknowledgements := &atomic.Int64{}
	transport := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			acknowledgements.Add(1)
		}
		f.fake.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(transport.Close)
	f.b.TG.Base = transport.URL
	denied, healthy := aliceCallback(9300, 100, "cancel::2"), message(9301, 202, "/orders")
	denied.Callback.From.LanguageCode = language
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.telegram_inbox(update_id,payload) VALUES($1,$2),($3,$4)`,
		denied.ID,
		denied,
		healthy.ID,
		healthy,
	)
	require.NoError(t, err)
	return f, provider, acknowledgements
}

func TestInactiveProviderUserCompletesDeniedUpdateWithoutReplay(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, provider, acks := providerInboxFixture(t, 1, language)
			completeInbox(t, f, 9302)
			var privateUpdates int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='alice'`).
					Scan(&privateUpdates),
			)
			require.Zero(t, privateUpdates, "disabled input must not enter private history")
			require.Positive(t, acks.Load(), "denied callback still needs acknowledgement")
			text, err := i18n.Translate(language, i18n.IdentityUnavailable, nil)
			require.NoError(t, err)
			notices := chatMessages(t, f, 101)
			require.Len(t, notices, 1)
			require.Equal(t, text, notices[0].Text)
			var cards int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner='bob'`).Scan(&cards),
			)
			require.Positive(t, cards, "healthy successor must receive its reply")
			provider.mode.Store(0)
			completeInbox(t, f, 9302)
			current, err := (workflow.Service{DB: f.db}).Current(t.Context(), "alice")
			require.NoError(t, err)
			assert.EqualValues(t, 2, current.Version, "reactivation must not execute rejected callback")
		})
	}
}

func TestProviderInfrastructureFailureKeepsDurableUpdate(t *testing.T) {
	t.Parallel()
	for _, mode := range []int32{2, 3, 4} {
		t.Run(
			map[int32]string{2: "outage", 3: "client credentials", 4: "unclassified rejection"}[mode],
			func(t *testing.T) {
				t.Parallel()
				f, provider, _ := providerInboxFixture(t, mode, "en")
				runInboxUntil(t, f, func() bool { return provider.calls.Load() > 0 })
				var pending int
				require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
				require.Equal(t, 2, pending)
				provider.mode.Store(0)
				completeInbox(t, f, 9302)
				current, err := (workflow.Service{DB: f.db}).Current(t.Context(), "alice")
				require.NoError(t, err)
				assert.EqualValues(
					t,
					3,
					current.Version,
					"transient failure must replay its original action after recovery",
				)
			},
		)
	}
}

func TestProviderUserDeactivatedAfterAdmissionIsTerminal(t *testing.T) {
	t.Parallel()
	f, provider, acknowledgements := providerInboxFixture(t, 5, "en")
	completeInbox(t, f, 9302)
	require.Positive(t, acknowledgements.Load())
	provider.mode.Store(0)
	completeInbox(t, f, 9302)
	current, err := (workflow.Service{DB: f.db}).Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.EqualValues(t, 2, current.Version)
}
