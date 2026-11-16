package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryOnboardingKeepsWelcomeAndDeferredSourceAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"replacement_deferred", "finish_after_discovery_welcome"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			testRecoveryOnboarding(t, mode == "replacement_deferred")
		})
	}
}

type recoveryOnboardingPeer struct {
	p           *peers
	replace     bool
	predecessor string
	accepted    chan struct{}
	release     chan struct{}
	receipt     *loom.RecoveryReceipt
}

func (f *recoveryOnboardingPeer) serve(w http.ResponseWriter, r *http.Request) {
	f.p.mu.Lock()
	switch r.URL.Path {
	case "/v1/capabilities":
		respondRecoveryCapabilities(w)
	case "/v1/chats/default":
		respond(w, map[string]string{"id": "retained-chat"})
	case "/v1/chats/retained-chat/active-run":
		located := loom.ActiveRun{Status: "idle", NextAction: "create_run"}
		if f.predecessor != "" {
			// Exercise a run finishing after discovery but before inbox acceptance.
			// Known-terminal discovery uses ordinary creation in the lifecycle test.
			located = loom.ActiveRun{RunID: f.predecessor, Status: "accepted", NextAction: "send_message"}
			if f.replace {
				located.Status, located.Reason, located.NextAction = "accepted", "budget_exhausted", "ask_user_to_continue"
			}
		}
		respond(w, located)
	case "/v1/chats/retained-chat/messages":
		if f.receipt == nil {
			run := loom.Run{ID: "onboarding-successor", ChatID: "retained-chat", AuthSessionID: "session",
				Status: "completed", Result: "Name saved", Interaction: loom.Interaction{ReplyMode: "agent"}}
			if f.replace {
				run.ReplacesRunID = f.predecessor
				for key, old := range f.p.runs {
					old.Status, old.Reason, old.Question = "cancelled", "superseded", nil
					f.p.runs[key] = old
				}
			}
			f.p.runs["onboarding-successor"] = run
			f.receipt = &loom.RecoveryReceipt{Accepted: true, Delivered: true, RunID: run.ID,
				MessageID: "name", Sequence: 1, Status: "queued", NextAction: "read_run"}
			close(f.accepted)
			f.p.mu.Unlock()
			disconnect(w)
			return
		}
		result := *f.receipt
		f.p.mu.Unlock()
		select {
		case <-f.release:
			respond(w, result)
		case <-r.Context().Done():
		}
		return
	default:
		f.p.mu.Unlock()
		f.p.agent(w, r)
		return
	}
	f.p.mu.Unlock()
}

func testRecoveryOnboarding(t *testing.T, replace bool) {
	t.Helper()
	p := newPeers(t)
	p.question = true
	f := &recoveryOnboardingPeer{p: p, replace: replace, accepted: make(chan struct{}), release: make(chan struct{})}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	text := "/start"
	if replace {
		text += " /profile"
	}
	first := h.submitText(t, 701, text)
	h.await(t, first, "awaiting_user")
	_, pending, token := agentRequestsName(t, h, first)
	row, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	f.predecessor = row.RunID
	if !replace {
		for key, run := range p.runs {
			run.Status, run.Question = "completed", nil
			p.runs[key] = run
		}
	}
	p.mu.Unlock()
	if !replace {
		h.await(t, first, "completed")
	}
	second := h.submitText(t, 702, "Anna Silva")
	select {
	case <-f.accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("name message was not accepted")
	}
	h.stop(t)
	code, raw := profileRequest(t, h, token, "complete_name", uuid.New(), map[string]any{
		"expected_version": pending.Version, "expectation_id": pending.NameExpectation.ID,
		"first_name": "Anna", "last_name": "Silva",
	})
	require.Equal(t, 200, code, string(raw))
	close(f.release)
	h.start(t)
	h.await(t, second, "completed")
	if replace {
		h.await(t, first, "closed")
		child := uuid.NewSHA1(first, []byte("start-continuation"))
		h.await(t, child, "completed")
		deliveredNamePrompt(t, h, child)
		h.stop(t)
		h.start(t)
		require.Equal(t, first, h.submitText(t, 701, text))
		var count int
		require.NoError(t, h.store.Pool().QueryRow(t.Context(),
			"SELECT count(*) FROM shuttle_tasks WHERE id=$1", child).Scan(&count))
		require.Equal(t, 1, count)
	} else {
		deliveredNamePrompt(t, h, second)
	}
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	welcome := false
	for _, sent := range p.sent {
		welcome = welcome || strings.Contains(sent.Text, "Hi! I’m ZiNuSya")
	}
	require.Equal(t, !replace, welcome)
	require.Len(t, p.createdInputs, 1, "the deferred /profile is native and must not create another paid run")
}
