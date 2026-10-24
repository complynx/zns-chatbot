package integration_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// The child runs the real worker in a separate process against the parent's
// isolated schema and mock HTTP peers. It must die without graceful cleanup.
func orderNoticeCrashChild(t *testing.T) {
	t.Helper()
	store, err := db.Open(t.Context(), os.Getenv("ZINUSYA_NOTICE_CRASH_DSN"))
	require.NoError(t, err)
	defer store.Close()
	client := &http.Client{Timeout: 20 * time.Second}
	iam, err := identity.New(identity.Options{
		Issuer: os.Getenv("ZINUSYA_NOTICE_CRASH_IAM"), OrganizationID: "org", BotID: 123,
		LoomAudience: "loom", APIAudience: "api", ActorID: "actor",
		ActorClientCredentials:         identity.ClientCredentials{ID: "actor-client", Secret: "secret"},
		ExchangeClientCredentials:      identity.ClientCredentials{ID: "exchange", Secret: "secret"},
		IntrospectionClientCredentials: identity.ClientCredentials{ID: "intro", Secret: "secret"},
	}, client)
	require.NoError(t, err)
	agent, err := loom.New(os.Getenv("ZINUSYA_NOTICE_CRASH_LOOM"), client)
	require.NoError(t, err)
	tg, err := telegram.New(os.Getenv("ZINUSYA_NOTICE_CRASH_TG"), "123:test-token", client)
	require.NoError(t, err)
	service, err := task.New(store, iam, agent, tg, nil, slog.New(slog.DiscardHandler), task.Options{
		BotID: 123, PollInterval: 50 * time.Millisecond, AttemptTimeout: 20 * time.Second,
		LeaseDuration: 30 * time.Second, MaxWorkers: 1,
	})
	require.NoError(t, err)
	require.NoError(t, service.Start(context.Background()))
	<-t.Context().Done()
}

func TestOrderNotificationProcessCrashQuarantinesUncertainSend(t *testing.T) {
	if os.Getenv("ZINUSYA_NOTICE_CRASH_CHILD") == "1" {
		orderNoticeCrashChild(t)
		return
	}
	t.Parallel()
	f, _, original := pendingOrderNotice(t)
	onlyOwnerOrderNotice(t, f)
	p := f.H.peers
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	p.mu.Lock()
	p.sendEntered, p.sendRelease = entered, release
	p.mu.Unlock()
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })

	dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	application := "zinusya_notice_crash_" + uuid.NewString()
	query := dsn.Query()
	query.Set("application_name", application)
	dsn.RawQuery = query.Encode()
	child := exec.CommandContext(
		t.Context(),
		os.Args[0],
		"-test.run=^TestOrderNotificationProcessCrashQuarantinesUncertainSend$",
		"-test.timeout=30s",
	)
	child.WaitDelay = time.Second
	child.Env = append(os.Environ(), "ZINUSYA_NOTICE_CRASH_CHILD=1",
		"ZINUSYA_NOTICE_CRASH_DSN="+dsn.String(), "ZINUSYA_NOTICE_CRASH_IAM="+p.iam.URL,
		"ZINUSYA_NOTICE_CRASH_LOOM="+p.loom.URL, "ZINUSYA_NOTICE_CRASH_TG="+p.tg.URL)
	var output bytes.Buffer
	child.Stdout, child.Stderr = &output, &output
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("child worker did not reach Telegram send")
	}
	scope := db.EventScope{BotID: 123, Organization: "org"}
	inflight, err := f.H.store.OrderNotification(t.Context(), scope, original.ID)
	require.NoError(t, err)
	require.Equal(t, "send_inflight", inflight.State)
	require.NotEqual(t, uuid.Nil, inflight.AttemptID)
	var jobID int64
	var crashedAttempt int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT id,attempt FROM river_job WHERE kind='zinusya_order_notification'" +
" AND args->>'notification_id'=$1 AND state='running'",
		original.ID.String()).
		Scan(&jobID, &crashedAttempt))
	require.NoError(t, child.Process.Kill())
	require.Error(t, child.Wait(), output.String())
	require.False(t, child.ProcessState.Success())
	unblock.Do(func() { close(release) })
	require.Len(t, sentNotifications(p, 11002), 1)

	require.Eventually(t, func() bool {
		var count int
		readErr := f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1", application).
			Scan(&count)
		return readErr == nil && count == 0
	}, 5*time.Second, 25*time.Millisecond)
	retained, err := f.H.store.OrderNotification(t.Context(), scope, original.ID)
	require.NoError(t, err)
	require.Equal(t, "send_inflight", retained.State)
	require.Equal(t, inflight.AttemptID, retained.AttemptID)
	f.H.start(t)
	quarantined := awaitOrderNotice(t, f, original.ID, "delivery_unknown")
	require.Equal(t, inflight.AttemptID, quarantined.AttemptID)
	require.Equal(t, original.Evidence, quarantined.Evidence)
	require.Equal(t, original.Order, quarantined.Order)
	// Startup quarantine must also survive the eventual replay of the crashed job.
	require.Eventually(t, func() bool {
		var state string
		var attempt int
		readErr := f.H.store.Pool().
			QueryRow(t.Context(), "SELECT state,attempt FROM river_job WHERE id=$1", jobID).
			Scan(&state, &attempt)
		return readErr == nil && state == "completed" && attempt > crashedAttempt
	}, 90*time.Second, 100*time.Millisecond)
	settled, err := f.H.store.OrderNotification(t.Context(), scope, original.ID)
	require.NoError(t, err)
	require.Equal(t, quarantined, settled)
	t.Log("Crashed queue job replay completed without changing quarantined delivery")

	require.Never(t, func() bool { return len(sentNotifications(p, 11002)) > 1 }, time.Second, 25*time.Millisecond)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	var reviews int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence_reviews WHERE evidence_id=$1", original.EvidenceID).
			Scan(&reviews),
	)
	require.Zero(t, reviews)
	p.mu.Lock()
	runCount := len(p.createKeys)
	p.mu.Unlock()
	require.Zero(t, runCount)
}
