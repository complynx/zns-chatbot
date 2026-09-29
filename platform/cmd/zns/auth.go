package main

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
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
	links := identity.Links{DB: db, Issuer: cfg.Auth.Zitadel.Issuer, BotID: botID}
	return api.ZitadelOwner(adapter, links), adapter, links, nil
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
	return verifyBotIdentity(ctx, b, cfg)
}
