package main

import (
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/stickerassets"
	"github.com/complynx/zns-chatbot/platform/internal/stickercache"
	"github.com/complynx/zns-chatbot/platform/internal/stickerclient"
)

func configureBotAssets(b *bot.Bot, cfg config.Config) error {
	origin, secret := cfg.Sticker.Worker.URL, cfg.Sticker.Worker.Secret.Value()
	if origin == "" && secret == "" {
		return nil
	}
	decoder, err := stickerclient.New(origin, secret)
	if err != nil {
		return err
	}
	options := stickercache.Options{Capacity: cfg.Sticker.Cache.Capacity, HalfLife: cfg.Sticker.Cache.HalfLife}
	cache, err := stickercache.New(b.DB, options)
	if err != nil {
		return err
	}
	b.Stickers = stickerassets.Service{Telegram: b.TG, Cache: cache, Decoder: decoder, Model: b.Model}
	return nil
}
