package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const provisionerTokenTimeout = 15 * time.Second
const sandboxMode = "sandbox"

func configureCoreProvisioning(
	ctx context.Context,
	next http.Handler,
	db *pgxpool.Pool,
	signer identity.Signer,
	cfg config.Config,
) (http.Handler, func(), error) {
	p := cfg.Auth.Zitadel.Provisioning
	if !p.Enabled {
		return next, func() {}, nil
	}
	if p.OrganizationID == "" || p.EmailDomain == "" || p.ClientID == "" || p.ClientSecret == "" {
		return nil, nil, errors.New("core provisioning configuration incomplete")
	}
	botID, err := strconv.ParseInt(cfg.Auth.Zitadel.BotID, 10, 64)
	if err != nil || botID <= 0 || botID >= 1<<52 {
		return nil, nil, errors.New("invalid provisioning bot identity")
	}
	client := &http.Client{
		Timeout:       provisionerTokenTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	tokenContext := context.WithValue(ctx, oauth2.HTTPClient, client)
	credentials := clientcredentials.Config{ClientID: p.ClientID, ClientSecret: p.ClientSecret.Value(),
		TokenURL: strings.TrimRight(cfg.Auth.Zitadel.Issuer, "/") + "/oauth/v2/token",
		Scopes:   []string{"urn:zitadel:iam:org:project:id:zitadel:aud"}, AuthStyle: oauth2.AuthStyleInHeader}
	sdk, err := identityprovision.NewSDK(identityprovision.SDKConfig{Issuer: cfg.Auth.Zitadel.Issuer,
		TokenSource: credentials.TokenSource(tokenContext), AllowLocalHTTP: cfg.Env == sandboxMode})
	if err != nil {
		return nil, nil, err
	}
	service := identityprovision.Service{
		DB:             db,
		Provider:       sdk,
		Issuer:         cfg.Auth.Zitadel.Issuer,
		Organization:   p.OrganizationID,
		BotID:          botID,
		EmailDomain:    p.EmailDomain,
		AllowLocalHTTP: cfg.Env == sandboxMode,
	}
	linkContext, cancelLinks := context.WithCancel(ctx)
	handler, err := configureAuthorizerProvisioning(
		linkContext,
		api.WithTelegramProvisioning(next, service, signer, botID),
		service,
		sdk,
		cfg,
	)
	if err != nil {
		cancelLinks()
		_ = sdk.Close()
		return nil, nil, err
	}
	return handler, func() { cancelLinks(); _ = sdk.Close() }, nil
}

func configureTrustedOnboarding(b *bot.Bot, cfg config.Config) error {
	if !cfg.Auth.Zitadel.Provisioning.Enabled {
		return nil
	}
	botID, err := strconv.ParseInt(cfg.Auth.Zitadel.BotID, 10, 64)
	if cfg.Auth.Mode != "zitadel" || err != nil || botID <= 0 || botID >= 1<<52 {
		return errors.New("invalid provisioning bot identity")
	}
	b.Onboarding = func(ctx context.Context, user telegram.User) error { return b.API.ProvisionTelegram(ctx, botID, user) }
	return nil
}
