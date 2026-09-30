package main

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func runtimeAuth(
	db *pgxpool.Pool,
	cfg config.Config,
	signer identity.Signer,
) (api.VerifyOwner, *identity.Zitadel, identity.Links, error) {
	switch cfg.Auth.Mode {
	case "", sandboxMode:
		return func(_ context.Context, token string) (string, error) { return signer.Verify(token) }, nil, identity.Links{}, nil
	case "zitadel":
	default:
		return nil, nil, identity.Links{}, errors.New("invalid runtime authentication mode")
	}
	adapter, err := identity.NewZitadel(cfg.Auth.Zitadel.AdapterConfig(cfg.Env))
	if err != nil {
		return nil, nil, identity.Links{}, err
	}
	botID, err := strconv.ParseInt(cfg.Auth.Zitadel.BotID, 10, 64)
	if err != nil || botID <= 0 {
		return nil, nil, identity.Links{}, errors.New("invalid runtime Telegram bot identity")
	}
	links := identity.Links{
		DB: db, Issuer: cfg.Auth.Zitadel.Issuer, BotID: botID,
		InvalidateSubject: adapter.InvalidateSubject,
	}
	return api.ZitadelOwner(adapter, links), adapter, links, nil
}

// observedRuntimeAuth registers the existing API adapter before composition.
func observedRuntimeAuth(
	db *pgxpool.Pool,
	cfg config.Config,
	signer identity.Signer,
	runtime *observability.Runtime,
) (api.VerifyOwner, *identity.Zitadel, identity.Links, error) {
	verify, adapter, links, err := runtimeAuth(db, cfg, signer)
	if err != nil {
		return nil, nil, identity.Links{}, err
	}
	if adapter != nil && runtime != nil {
		if err = runtime.RegisterIdentityCaches(observability.IdentityCacheAPI, adapter); err != nil {
			return nil, nil, identity.Links{}, err
		}
	}
	return verify, adapter, links, nil
}

func configureBotAuth(ctx context.Context, b *bot.Bot, cfg config.Config, signer identity.Signer) error {
	_, adapter, links, err := runtimeAuth(b.DB, cfg, signer)
	if err != nil {
		return err
	}
	if adapter != nil {
		b.API.Exchange = adapter
		b.API.Links = links
	}
	if cfg.Env == sandboxMode {
		b.API.SandboxToken = signer.Token
	}
	b.Host = appclient.Host{Base: b.API.Base, HTTP: b.API.HTTP, Signer: signer, UserToken: b.API.UserToken}
	if err = verifyBotIdentity(ctx, b, cfg); err != nil {
		return err
	}
	if adapter != nil {
		if runtime, ok := b.Observer.(*observability.Runtime); ok && runtime != nil {
			return runtime.RegisterIdentityCaches(observability.IdentityCacheBot, adapter)
		}
	}
	return nil
}
