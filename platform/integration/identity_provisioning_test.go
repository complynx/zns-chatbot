package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type provisioningProvider struct {
	mu                                sync.Mutex
	accounts                          map[string]identityprovision.Account
	creates                           int
	lostResponse, failReadAfterCreate bool
	readFailed                        bool
}

func (p *provisioningProvider) Get(_ context.Context, id string) (identityprovision.Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failReadAfterCreate && p.creates > 0 && !p.readFailed {
		p.readFailed = true
		return identityprovision.Account{}, identityprovision.ErrUnavailable
	}
	account, ok := p.accounts[id]
	if !ok {
		return account, identityprovision.ErrNotFound
	}
	return account, nil
}

func (p *provisioningProvider) Create(_ context.Context, c identityprovision.Creation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creates++
	if _, ok := p.accounts[c.Subject]; ok {
		return identityprovision.ErrConflict
	}
	p.accounts[c.Subject] = identityprovision.Account{
		Subject:      c.Subject,
		Organization: c.Organization,
		Operation:    c.Operation,
		Active:       true,
		Human:        true,
	}
	if p.lostResponse {
		return identityprovision.ErrUnavailable
	}
	return nil
}

func TestIdentityProvisioningConcurrentReplay(t *testing.T) {
	t.Parallel()
	db := database(t)
	provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}, lostResponse: true}
	service := identityprovision.Service{
		DB:           db,
		Provider:     provider,
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        999,
		EmailDomain:  "telegram.invalid",
	}
	input := identityprovision.Telegram{ID: 95101, FirstName: "Synthetic", LastName: "Person", Language: "ru"}
	bindings := make(chan identityprovision.Binding, 8)
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() { b, err := service.EnsureTelegram(t.Context(), input); bindings <- b; errors <- err })
	}
	workers.Wait()
	close(bindings)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var expected identityprovision.Binding
	for b := range bindings {
		if expected.Owner == "" {
			expected = b
		}
		require.Equal(t, expected, b)
	}
	require.Equal(t, 1, provider.creates)
	var allowed, ready bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT u.can_book,p.ready FROM core.users u JOIN core.identity_provisioning p ON p.owner=u.id WHERE u.id=$1`, expected.Owner).
			Scan(&allowed, &ready),
	)
	require.True(t, allowed)
	require.True(t, ready)
	_, err := db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id=$1`, expected.Owner)
	require.NoError(t, err)
	input.FirstName = "Changed profile"
	again, err := service.EnsureTelegram(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, expected, again)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT can_book FROM core.users WHERE id=$1`, expected.Owner).Scan(&allowed),
	)
	require.False(t, allowed, "replay must preserve revoked policy")
}

func TestIdentityProvisioningResumeAfterProviderCommit(t *testing.T) {
	t.Parallel()
	db := database(t)
	provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}, failReadAfterCreate: true}
	service := identityprovision.Service{
		DB:           db,
		Provider:     provider,
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        999,
		EmailDomain:  "telegram.invalid",
	}
	input := identityprovision.Telegram{ID: 95102, FirstName: "Synthetic", Language: "en"}
	_, err := service.EnsureTelegram(t.Context(), input)
	require.ErrorIs(t, err, identityprovision.ErrUnavailable)
	var owner, subject string
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT owner,subject FROM core.identity_provisioning WHERE telegram_id=$1`, input.ID).
			Scan(&owner, &subject),
	)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.users WHERE telegram_id=$1`, input.ID).Scan(&count),
	)
	require.Zero(t, count, "failure must not expose a partial local user")
	// A fresh service instance represents process restart; it reads the journal.
	restarted := service
	b, err := restarted.EnsureTelegram(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, identityprovision.Binding{Owner: owner, Subject: subject}, b)
	require.Equal(t, 1, provider.creates)
	var name, lastName string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT name,last_name FROM core.users WHERE id=$1`, owner).Scan(&name, &lastName),
	)
	require.Equal(t, "Synthetic", name)
	require.Empty(t, lastName, "provider-required family-name fallback must not alter Telegram profile")
	provider.mu.Lock()
	account := provider.accounts[subject]
	account.Active = false
	provider.accounts[subject] = account
	provider.mu.Unlock()
	_, err = service.EnsureTelegram(t.Context(), input)
	require.ErrorIs(t, err, identityprovision.ErrConflict)
	require.Equal(t, 1, provider.creates, "disabled users must not be recreated")
}

func TestIdentityProvisioningPrepareAndImport(t *testing.T) {
	t.Parallel()
	db := database(t)
	provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}}
	service := identityprovision.Service{
		DB:           db,
		Provider:     provider,
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        999,
		EmailDomain:  "telegram.invalid",
	}
	input := identityprovision.Telegram{ID: 95103, FirstName: "Synthetic", Language: "en"}
	b, err := service.PrepareTelegram(t.Context(), input)
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.users WHERE telegram_id=$1`, input.ID).Scan(&count),
	)
	require.Zero(t, count, "pre-stage must leave users to the existing importer")
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.users(id,telegram_id,name,can_book) VALUES($1,$2,'Imported',false)`,
		b.Owner,
		input.ID,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES($1,$2,$3)`,
		b.Owner,
		service.Issuer,
		b.Subject,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.telegram_identities(bot_id,telegram_id,owner) VALUES($1,$2,$3)`,
		service.BotID,
		input.ID,
		b.Owner,
	)
	require.NoError(t, err)
	again, err := service.EnsureTelegram(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, b, again)
	var allowed bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT can_book FROM core.users WHERE id=$1`, b.Owner).Scan(&allowed))
	require.False(t, allowed)
}

func TestIdentityProvisioningConflicts(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"operation", "organization", "human", "subject"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}}
			service := identityprovision.Service{
				DB:           db,
				Provider:     provider,
				Issuer:       "https://identity.invalid",
				Organization: "test-org",
				BotID:        999,
				EmailDomain:  "telegram.invalid",
			}
			input := identityprovision.Telegram{ID: 95104, FirstName: "Synthetic"}
			b, err := service.PrepareTelegram(t.Context(), input)
			require.NoError(t, err)
			account := provider.accounts[b.Subject]
			switch field {
			case "operation":
				account.Operation = "foreign"
			case "organization":
				account.Organization = "foreign"
			case "human":
				account.Human = false
			case "subject":
				account.Subject = "foreign"
			}
			provider.accounts[b.Subject] = account
			_, err = service.EnsureTelegram(t.Context(), input)
			require.ErrorIs(t, err, identityprovision.ErrConflict)
			var count int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.users WHERE telegram_id=$1`, input.ID).Scan(&count),
			)
			assert.Zero(t, count)
			assert.Equal(t, 1, provider.creates)
		})
	}
}

func TestIdentityProvisioningUnlinkedExistingUser(t *testing.T) {
	t.Parallel()
	db := database(t)
	provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}}
	service := identityprovision.Service{
		DB:           db,
		Provider:     provider,
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        999,
		EmailDomain:  "telegram.invalid",
	}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.users(id,telegram_id,name) VALUES('unlinked-identity',95105,'Existing')`,
	)
	require.NoError(t, err)
	_, err = service.EnsureTelegram(t.Context(), identityprovision.Telegram{ID: 95105})
	require.ErrorIs(t, err, identityprovision.ErrConflict)
	require.Zero(t, provider.creates, "an existing local user must not acquire a second provider account")
}

func TestIdentityProvisioningSQLFailureKeepsReservation(t *testing.T) {
	t.Parallel()
	db := database(t)
	provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}}
	service := identityprovision.Service{
		DB:           db,
		Provider:     provider,
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        999,
		EmailDomain:  "telegram.invalid",
	}
	input := identityprovision.Telegram{ID: 95106, FirstName: "Synthetic"}
	_, err := db.Exec(
		t.Context(),
		`CREATE FUNCTION core.reject_provisioning_test() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'synthetic transaction failure' USING ERRCODE='40001'; END $$;
 CREATE TRIGGER reject_provisioning_test BEFORE INSERT ON core.users FOR EACH ROW EXECUTE FUNCTION core.reject_provisioning_test()`,
	)
	require.NoError(t, err)
	_, err = service.EnsureTelegram(t.Context(), input)
	var databaseError *pgconn.PgError
	require.ErrorAs(t, err, &databaseError)
	require.Equal(t, "40001", databaseError.Code)
	require.NotErrorIs(t, err, identityprovision.ErrConflict)
	var reserved identityprovision.Binding
	var ready bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT owner,subject,ready FROM core.identity_provisioning WHERE telegram_id=$1`, input.ID).
			Scan(&reserved.Owner, &reserved.Subject, &ready),
	)
	require.False(t, ready)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.users WHERE telegram_id=$1`, input.ID).Scan(&count),
	)
	require.Zero(t, count)
	_, err = db.Exec(t.Context(), `DROP TRIGGER reject_provisioning_test ON core.users`)
	require.NoError(t, err)
	actual, err := service.EnsureTelegram(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, reserved, actual)
	require.Equal(t, 1, provider.creates, "remote success must be recovered without a second account")
}

type provisioningExchange struct {
	links  identity.Links
	signer identity.Signer
}

func (e provisioningExchange) Exchange(ctx context.Context, subject string) (string, error) {
	owner, err := e.links.Subject(ctx, subject)
	if err != nil {
		return "", err
	}
	return e.signer.Token(owner), nil
}

func provisioningTelegram(t *testing.T, allowed ...int64) (telegram.Client, *atomic.Int64) {
	t.Helper()
	delivered := &atomic.Int64{}
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botsynthetic/setChatMenuButton", "/botsynthetic/setMyCommands":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
			return
		}
		var send telegram.Send
		if json.NewDecoder(r.Body).Decode(&send) != nil || !slices.Contains(allowed, send.ChatID) {
			http.Error(w, "unexpected synthetic destination", http.StatusBadRequest)
			return
		}
		messageID := delivered.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": telegram.Message{
			ID: messageID, Text: send.Text, Chat: telegram.Chat{ID: send.ChatID, Type: "private"},
		}})
	}))
	t.Cleanup(telegramServer.Close)
	return telegram.Client{Base: telegramServer.URL, Token: "synthetic"}, delivered
}

func TestIdentityProvisioningFirstMessageContinues(t *testing.T) {
	t.Parallel()
	f := setup(t)
	client, delivered := provisioningTelegram(t, 95107)
	f.b.TG = client
	provider := &provisioningProvider{accounts: map[string]identityprovision.Account{}}
	service := identityprovision.Service{
		DB:           f.db,
		Provider:     provider,
		Issuer:       "https://identity.invalid",
		Organization: "test-org",
		BotID:        999,
		EmailDomain:  "telegram.invalid",
	}
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	links := identity.Links{DB: f.db, Issuer: service.Issuer, BotID: service.BotID}
	server := httptest.NewServer(api.WithTelegramProvisioning(
		api.Handler(
			appservices.NewServices(f.db, appservices.Options{}),
			signer,
			slog.New(slog.DiscardHandler),
		),
		service,
		signer,
		service.BotID,
	))
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
	onboardingCalls := 0
	f.b.Onboarding = func(ctx context.Context, user telegram.User) error {
		onboardingCalls++
		return f.b.Host.ProvisionTelegram(ctx, service.BotID, user)
	}
	update := message(9100, 95107, "/orders")
	update.Message.From.FirstName = "Synthetic"
	update.Message.From.LanguageCode = "en"
	handle(t, f.b, update)
	bound, err := links.Telegram(t.Context(), 95107)
	require.NoError(t, err)
	var allowed bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT can_book FROM core.users WHERE id=$1`, bound.Owner).Scan(&allowed),
	)
	require.True(t, allowed)
	var cards int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner=$1`, bound.Owner).Scan(&cards),
	)
	require.Positive(t, cards, "the first request must continue to its ordinary orders menu")
	require.Positive(t, delivered.Load())
	handle(t, f.b, update)
	restarted := *f.b
	update.ID++
	update.Message.ID++
	handle(t, &restarted, update)
	require.Equal(t, 1, onboardingCalls)
	require.Equal(t, 1, provider.creates)
}
