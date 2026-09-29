package main

import (
	"log/slog"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func configureBotLineup(b *bot.Bot, cfg config.Config) {
	if cfg.Lineup.CSV == "" {
		return
	}
	source, err := agent.LoadLineup(cfg.Lineup.CSV, cfg.Lineup.EventYear, cfg.Lineup.Timezone)
	b.Lineup = source
	if err != nil {
		logger := b.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("DJ lineup unavailable", "reason", err.Error())
	}
}
