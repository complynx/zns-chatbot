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
	require.True(t, strings.HasPrefix(db.Config().ConnConfig.Database, "zns_test_"))
	b := &bot.Bot{
		DB: db,
		API: bot.APIClient{Base: os.Getenv("ZNS_INBOX_PROCESS_TEST_API"),
			Signer: identity.Signer{Key: []byte(strings.Repeat("k", 32))}},
		TG: telegram.Client{Base: os.Getenv("ZNS_INBOX_PROCESS_TEST_TG"), Token: "sandbox"},
		Model: avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
			_, printErr := fmt.Fprintln(os.Stdout, inboxDurableMarker)
			if printErr != nil {
				return agent.Plan{}, printErr
			}
			<-ctx.Done()
			return agent.Plan{}, ctx.Err()
		}),
	}
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
	upstream, err := f.b.TG.Updates(t.Context(), 3)
	require.NoError(t, err)
	require.Empty(t, upstream)
	require.Eventually(t, func() bool {
		var released bool
		queryErr := f.db.QueryRow(t.Context(), `SELECT pg_try_advisory_xact_lock(918273)`).Scan(&released)
		return queryErr == nil && released
	}, 5*time.Second, 10*time.Millisecond)
	completeInbox(t, f, 3)
	assert.Equal(t, 2, f.model.calls)
}
