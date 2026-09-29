package integration_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestMassageTimetableBrowser(t *testing.T) {
	t.Parallel()
	if os.Getenv("TIMETABLE_BROWSER") != "1" {
		t.Skip("set TIMETABLE_BROWSER=1 and NODE_BINARY for isolated browser test")
	}
	f := massageBotFixture(t)
	_, err := (massage.Service{DB: f.db}).Execute(t.Context(), "alice", massageBook("browser", "bob", 2, 1))
	require.NoError(t, err)
	gateway := miniapp.Gateway{API: f.b.API, Token: "sandbox", EventID: "sandbox-festival"}
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	fake, err := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, err)
	fake.MiniAppURL = server.URL
	chat := httptest.NewServer(fake.Handler())
	t.Cleanup(chat.Close)
	f.b.TG = telegram.Client{Base: chat.URL, Token: "sandbox"}
	f.b.WebAppURL = server.URL + "/miniapp/"
	handle(t, f.b, message(9910, 202, "/massage"))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	node := os.Getenv("NODE_BINARY")
	if node == "" {
		node = "node"
	}
	command := exec.CommandContext(ctx, node, "tests/massage-timetable.mjs")
	command.Dir = ".."
	command.Env = append(os.Environ(), "TIMETABLE_URL="+server.URL, "SANDBOX_URL="+chat.URL,
		"TIMETABLE_STAFF="+sandbox.WebAppInitData(telegram.User{ID: 202}, "sandbox", time.Now()),
		"TIMETABLE_CLIENT="+sandbox.WebAppInitData(telegram.User{ID: 101}, "sandbox", time.Now()))
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Log(string(output))
}
