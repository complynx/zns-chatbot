package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const inboxProcessDB = "ZNS_INBOX_PROCESS_TEST_DB"
const inboxDurableMarker = "INBOX_DURABLE"

func inboxChild(t *testing.T, connection string) {
	t.Helper()
	db, err := pgxpool.New(t.Context(), connection)
	require.NoError(t, err)
	defer db.Close()
	require.True(t, strings.HasPrefix(db.Config().ConnConfig.Database, "synthetic_qa_zns_"))
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	b := &bot.Bot{
		DB: db,
		API: appclient.Client{Base: os.Getenv("ZNS_INBOX_PROCESS_TEST_API"),
			SandboxToken: signer.Token},
		Host: appclient.Host{Base: os.Getenv("ZNS_INBOX_PROCESS_TEST_API"), Signer: signer},
		TG:   telegram.Client{Base: os.Getenv("ZNS_INBOX_PROCESS_TEST_TG"), Token: "sandbox"},
		Model: avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
			_, printErr := fmt.Fprintln(os.Stdout, inboxDurableMarker)
			if printErr != nil {
				return agent.Plan{}, printErr
			}
			<-ctx.Done()
			return agent.Plan{}, ctx.Err()
		}),
	}
	b.Host.UserToken = b.API.UserToken
	require.NoError(t, b.Run(t.Context()))
}

func TestInboxCrashRecovery(t *testing.T) {
	if connection := os.Getenv(inboxProcessDB); connection != "" {
		inboxChild(t, connection)
		return
	}
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "first"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "second"})
	batch, err := f.b.TG.Updates(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, batch, 2)
	executable, err := os.Executable()
	require.NoError(t, err)
	connection, err := url.Parse(f.db.Config().ConnString())
	require.NoError(t, err)
	connection.Path = "/" + f.db.Config().ConnConfig.Database
	child := exec.CommandContext(t.Context(), executable, "-test.run=^TestInboxCrashRecovery$")
	child.Env = append(os.Environ(), inboxProcessDB+"="+connection.String(),
		"ZNS_INBOX_PROCESS_TEST_API="+f.b.API.Base, "ZNS_INBOX_PROCESS_TEST_TG="+f.fake.URL)
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == inboxDurableMarker {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case reached := <-ready:
		require.True(t, reached, "child must reach model processing after durable intake")
	case <-time.After(10 * time.Second):
		t.Fatal("child did not reach the crash barrier")
	}
	// Kill bypasses all signal handlers, defers and graceful-shutdown hooks.
	require.NoError(t, child.Process.Kill())
	require.Error(t, child.Wait())
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Equal(t, 2, pending)
	require.Equal(t, batch, retryInboxPayloads(t, f), "process death preserves the complete received payloads")
	var failures int
	var state string
	var deadline time.Time
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT failures,state,next_attempt_at FROM bot.telegram_inbox WHERE update_id=1").
		Scan(&failures, &state, &deadline))
	require.Zero(t, failures, "process death is not a conclusive handler failure")
	require.Equal(t, "pending", state)
	upstream, err := f.b.TG.Updates(t.Context(), 3)
	require.NoError(t, err)
	require.Empty(t, upstream)
	require.Equal(t, batch, retryInboxPayloads(t, f), "upstream acknowledgement does not lose either update")
	require.Eventually(t, func() bool {
		var released bool
		queryErr := f.db.QueryRow(t.Context(), `SELECT pg_try_advisory_xact_lock(918273)`).Scan(&released)
		return queryErr == nil && released
	}, 5*time.Second, 10*time.Millisecond)
	var seen []string
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		seen = append(seen, input.Text)
		return f.model.Plan(t.Context(), input)
	})
	stop := startRetryInbox(t, f)
	require.Eventually(t, func() bool {
		var cursor int64
		var count int
		queryErr := f.db.QueryRow(t.Context(), `SELECT value,(SELECT count(*) FROM bot.telegram_inbox)
FROM bot.cursors WHERE name='telegram'`).Scan(&cursor, &count)
		return queryErr == nil && cursor == 3 && count == 0
	}, max(time.Until(deadline), 0)+5*time.Second, 10*time.Millisecond)
	stop()
	assert.Equal(t, []string{"first", "second"}, seen)
	assert.Equal(t, 2, f.model.calls)
}
