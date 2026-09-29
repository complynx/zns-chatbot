package main

import (
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func configureBrowserAuth(b *bot.Bot, cfg config.Config, localURL string) error {
	publicURL := cfg.Telegram.WebAppURL
	if publicURL == "" && (cfg.Auth.Mode == sandboxMode || cfg.Auth.Mode == "") {
		publicURL = localURL
	}
	if publicURL == "" {
		return nil
	}
	service, err := browserauth.New(
		b.DB,
		b.API.Signer,
		b.TG,
		publicURL,
		b.API.BrowserAuthRecipient,
		b.API.AuthenticateTelegram,
	)
	if err != nil {
		return err
	}
	if err = service.ConfigureLegacyOrigins(cfg.Auth.LegacyBrowserOrigins); err != nil {
		return err
	}
	b.BrowserAuth = service
	return nil
}
