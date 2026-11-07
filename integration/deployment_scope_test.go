package integration_test

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

//nolint:paralleltest,tparallel // Restart attempts share one queue; concurrency would test the instance lock instead.
func TestRestartRejectsDifferentDeploymentBeforeRetryingDelivery(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.rejectSendOnce = true
	h := setup(t, p)
	id := h.submit(t, 1)
	require.Eventually(t, func() bool {
		rows, err := h.store.ListOutbox(context.Background(), id)
		return err == nil && len(rows) == 1 && rows[0].State == "retryable"
	}, 5*time.Second, 20*time.Millisecond)
	h.stop(t)
	p.mu.Lock()
	originalAttempts := len(p.sent)
	p.mu.Unlock()
	for _, change := range []struct {
		name        string
		bot         int64
		org, issuer string
	}{
		{"bot", 124, "org", p.iam.URL},
		{"organization", 123, "other-org", p.iam.URL},
		{"issuer", 123, "org", "http://127.0.0.1:1"},
	} {
		t.Run(change.name, func(t *testing.T) {
			iam, err := identity.New(identity.Options{
				BotID: change.bot, OrganizationID: change.org, Issuer: change.issuer,
				LoomAudience: "loom", APIAudience: "api", ActorID: "actor",
				ActorClientCredentials:         identity.ClientCredentials{ID: "actor-client", Secret: "secret"},
				ExchangeClientCredentials:      identity.ClientCredentials{ID: "exchange", Secret: "secret"},
				IntrospectionClientCredentials: identity.ClientCredentials{ID: "intro", Secret: "secret"},
			}, p.iam.Client())
			require.NoError(t, err)
			agent, err := loom.New(p.loom.URL, p.loom.Client())
			require.NoError(t, err)
			tg, err := telegram.New(p.tg.URL, strconv.FormatInt(change.bot, 10)+":test-token", p.tg.Client())
			require.NoError(t, err)
			service, err := task.New(h.store, iam, agent, tg, nil, slog.New(slog.DiscardHandler), task.Options{
				BotID: change.bot, MaxWorkers: 2, PollInterval: 50 * time.Millisecond,
				AttemptTimeout: 2 * time.Second, LeaseDuration: 3 * time.Second,
			})
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, service.Stop(ctx))
			})
			require.ErrorIs(t, service.Start(context.Background()), db.ErrDeploymentScope)
			p.mu.Lock()
			attempts := len(p.sent)
			p.mu.Unlock()
			require.Equal(t, originalAttempts, attempts)
			rows, err := h.store.ListOutbox(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, "retryable", rows[0].State)
		})
	}
	h.start(t)
	require.Eventually(t, func() bool {
		rows, err := h.store.ListOutbox(context.Background(), id)
		return err == nil && len(rows) == 1 && rows[0].State == "delivered"
	}, 5*time.Second, 20*time.Millisecond)
}
