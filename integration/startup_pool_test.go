package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func startupPool(t *testing.T, h *harness, maximum string) *db.Store {
	t.Helper()
	parsed, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", maximum)
	query.Set("pool_min_conns", "0")
	parsed.RawQuery = query.Encode()
	store, err := db.Open(t.Context(), parsed.String())
	require.NoError(t, err)
	return store
}

func poolService(h *harness, store *db.Store) (*task.Service, error) {
	agent, err := loom.New(h.peers.loom.URL, h.peers.loom.Client())
	if err != nil {
		return nil, err
	}
	tg, err := telegram.New(h.peers.tg.URL, "123:test-token", h.peers.tg.Client())
	if err != nil {
		return nil, err
	}
	return task.New(store, h.iam, agent, tg, nil, slog.New(slog.DiscardHandler), task.Options{
		BotID:          123,
		MaxWorkers:     2,
		PollInterval:   50 * time.Millisecond,
		AttemptTimeout: 2 * time.Second,
		LeaseDuration:  3 * time.Second,
	})
}

func TestTaskStartupRejectsSingleConnectionAndRecoversWithTwo(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	h.stop(t)
	single := startupPool(t, h, "1")
	defer single.Close()
	service, err := poolService(h, single)
	require.ErrorContains(t, err, "at least two connections")
	require.Nil(t, service)
	// A refused task configuration must leave standalone DB/API use available.
	require.NoError(t, single.Pool().Ping(t.Context()))
	two := startupPool(t, h, "2")
	h.store = two
	t.Cleanup(func() { h.stop(t); two.Close() })
	service, err = poolService(h, two)
	require.NoError(t, err)
	h.service = service
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, service.Start(ctx), "Rejected startup must not retain the instance lease")
	update := telegram.Update{
		ID: 8001,
		Message: &telegram.Message{ID: 1, From: &telegram.User{ID: 456, FirstName: "Pool fixture"},
			Chat: telegram.Chat{ID: 456, Type: "private"}, Text: "Hello from minimal pool"},
	}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, duplicate, err := service.Accept(ctx, update, body)
	require.NoError(t, err)
	require.False(t, duplicate)
	h.await(t, accepted.ID, "completed")
	h.awaitReplies(t, 1)
	// Keep the old pool open while a fresh pool checks the same instance lease.
	h.stop(t)
	fresh := startupPool(t, h, "2")
	h.store = fresh
	t.Cleanup(func() { h.stop(t); fresh.Close() })
	replacement, err := poolService(h, fresh)
	require.NoError(t, err)
	h.service = replacement
	require.NoError(t, replacement.Start(ctx))
}
