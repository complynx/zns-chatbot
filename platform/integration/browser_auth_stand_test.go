package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
)

// Opt-in disposable UI stand; never runs in the normal test gate.
func TestBrowserAuthUIStand(t *testing.T) {
	t.Parallel()
	output := os.Getenv("BROWSER_AUTH_STAND_FILE")
	if output == "" {
		t.Skip("set BROWSER_AUTH_STAND_FILE for an ephemeral interactive stand")
	}
	lifetime := 10 * time.Minute
	if configured := os.Getenv("BROWSER_AUTH_STAND_LIFETIME"); configured != "" {
		var err error
		lifetime, err = time.ParseDuration(configured)
		require.NoError(t, err)
		require.Positive(t, lifetime)
	}
	f := massageBotFixture(t)
	seedBrowserFood(t, f)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET username=id WHERE id IN ('alice','bob')`)
	require.NoError(t, err)
	order := intakeOrder(t, f, "browser-ui-order")
	server := httptest.NewUnstartedServer(nil)
	prefix := os.Getenv("BROWSER_AUTH_STAND_PREFIX")
	publicURL := "http://" + server.Listener.Addr().String() + prefix + "/miniapp/"
	service, err := browserauth.New(
		f.db,
		f.b.Host.Signer,
		f.b.TG,
		publicURL,
		f.b.Host.BrowserAuthRecipient,
		f.b.API.AuthenticateTelegram,
	)
	require.NoError(t, err)
	f.b.BrowserAuth = service
	server.Config.Handler = http.StripPrefix(prefix, (miniapp.Gateway{
		WebAppURL: publicURL, BrowserAuth: service, API: f.b.API, Token: "sandbox", EventID: "sandbox-festival",
	}).Handler())
	server.Start()
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- f.b.Run(ctx) }()
	data, err := json.Marshal(map[string]string{
		"browser": server.URL + prefix, "telegram": f.fake.URL, "order": order.ID,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(output, data, 0o600))
	t.Log(string(data))
	select {
	case err = <-ended:
		require.NoError(t, err)
	case <-time.After(lifetime):
		cancel()
		require.NoError(t, <-ended)
	}
}

func seedBrowserFood(t *testing.T, f *fixture) {
	t.Helper()
	server := httptest.NewServer(api.Handler(appservices.NewServices(f.db, appservices.Options{LegacyOrderBotID: 77}),
		f.b.Host.Signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	f.b.API.Base = server.URL
	f.b.Host.Base = f.b.API.Base
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at) VALUES('food-browser','2035-01-01Z');
 INSERT INTO core.food_events(event_id,bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after)
 VALUES('food-browser',77,'{"friday":{"dinner":[{"title_ru":"Рыба","title_en":"Fish","price":185,"photo":"fish"}]}}',repeat('a',64),
 '{"with_soup":665,"without_soup":555}','{}','2035-01-01Z',38,'7 days','1 day','1 hour')`)
	require.NoError(t, err)
}
