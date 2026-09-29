package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestConfiguredTelegramRequiresDurableControlPolicy(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{
		BotID: 77, BotInterval: time.Millisecond, ChatInterval: time.Second, Fallback: time.Second,
	}
	cfg := config.Config{}
	cfg.Telegram.BaseURL = "https://api.telegram.org"
	_, err := configuredTelegramClient(cfg, nil, nil, settings)
	require.Error(t, err)
	_, err = configuredTelegramClient(cfg, nil, &pgxpool.Pool{}, delivery.Settings{})
	require.Error(t, err)
	pool, err := pgxpool.New(
		context.Background(),
		"postgres://synthetic:synthetic@127.0.0.1:1/synthetic_qa_zns_constructor?sslmode=disable",
	)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	client, err := configuredTelegramClient(cfg, nil, pool, settings)
	require.NoError(t, err)
	require.NotNil(t, client.Control)
	require.Equal(t, cfg.Telegram.BaseURL, client.Base)
}
