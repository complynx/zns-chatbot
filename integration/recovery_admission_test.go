package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryNoRunRediscoveryCreatesOriginalMessageOnce(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	inboxCalls := 0
	var creationKey string
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		located := loom.ActiveRun{RunID: "vanished", Status: "accepted", NextAction: "send_message"}
		if inboxCalls > 0 {
			located = loom.ActiveRun{Status: "idle", NextAction: "create_run"}
		}
		if respondRecoveryDiscovery(w, r, located) {
			p.mu.Unlock()
			return
		}
		if r.URL.Path == "/v1/chats/retained-chat/messages" {
			inboxCalls++
			p.mu.Unlock()
			w.WriteHeader(http.StatusConflict)
			respond(w, map[string]any{"error": map[string]string{"code": "no_run"}})
			return
		}
		if r.URL.Path == "/v1/runs" && r.Method == http.MethodPost {
			creationKey = r.Header.Get("Idempotency-Key")
		}
		p.mu.Unlock()
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	id := h.submitText(t, 1241, "Original message after a disappearing run")
	h.await(t, id, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, inboxCalls)
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], "Original message after a disappearing run")
	require.Len(t, p.createAttempts, 1)
	require.Equal(t, id.String(), creationKey)
}
