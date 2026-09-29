package integration_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type liveState struct {
	Messages []telegram.Message `json:"messages"`
	Edits    int                `json:"edits"`
}

func awaitCard(t *testing.T, url string, predicate func(telegram.Message) bool) telegram.Message {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, e := http.Get(url + "/lab/state?user=101")
		require.NoError(t, e)

		var s liveState
		e = json.NewDecoder(resp.Body).Decode(&s)
		resp.Body.Close()
		require.NoError(t, e)

		for _, m := range s.Messages {
			if m.From.IsBot && !strings.Contains(m.Text, "Заказ") && predicate(m) {
				return m
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("live sandbox did not reach expected state")
	return telegram.Message{}
}
func clickLive(t *testing.T, url string, m telegram.Message, prefix string) {
	t.Helper()
	for _, row := range m.Markup.Rows {
		for _, b := range row {
			if strings.HasPrefix(b.Data, prefix) {
				post(t, url+"/lab/input", map[string]any{"user": 101, "data": b.Data, "message_id": m.ID})
				return
			}
		}
	}
	t.Fatalf("button %s not found", prefix)
}

//nolint:paralleltest // This test mutates the shared running sandbox.
func TestLiveSandbox(t *testing.T) {
	url := os.Getenv("SANDBOX_URL")
	if url == "" {
		t.Skip("SANDBOX_URL is required for live container smoke")
	}
	post(t, url+"/lab/input", map[string]any{"user": 101, "text": "/start"})
	m := awaitCard(t, url, func(_ telegram.Message) bool { return true })
	if strings.Contains(m.Text, "Статус: Забронировано") || strings.Contains(m.Text, "Статус: Черновик") {
		clickLive(t, url, m, "cancel:")
		m = awaitCard(t, url, func(m telegram.Message) bool { return strings.Contains(m.Text, "Статус: Отменено") })
	}
	clickLive(t, url, m, "select:shuttle-1:")
	m = awaitCard(t, url, func(m telegram.Message) bool { return strings.Contains(m.Text, "Статус: Черновик") })
	original := m.ID
	post(t, url+"/lab/input", map[string]any{"user": 101, "text": "помоги закончить"})
	m = awaitCard(t, url, func(m telegram.Message) bool { return strings.Contains(m.Text, "Черновик сохранён") })
	clickLive(t, url, m, "confirm:")
	m = awaitCard(t, url, func(m telegram.Message) bool {
		return strings.Contains(m.Text, "Статус: Забронировано")
	})
	if m.ID != original {
		t.Fatal("manual card was not updated in place")
	}
	clickLive(t, url, m, "cancel:")
	awaitCard(t, url, func(m telegram.Message) bool { return strings.Contains(m.Text, "Статус: Отменено") })
}
