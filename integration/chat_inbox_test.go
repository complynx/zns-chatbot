package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const inboxChat = "chat-1"

// serveLoom puts handle before the standard Loom peer. handle returns false to
// pass a request to the standard peer. It must not hold p.mu when it returns false.
func serveLoom(t *testing.T, p *peers, handle func(http.ResponseWriter, *http.Request) bool) {
	t.Helper()
	p.loom.Close()
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !handle(w, r) {
			p.agent(w, r)
		}
	}))
	t.Cleanup(p.loom.Close)
}

func rejectLoom(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":{"code":"`+code+`"}}`)
}

// awaitLastReply waits until the newest Telegram message has the text.
func awaitLastReply(t *testing.T, p *peers, text string) {
	t.Helper()
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.sent) > 0 && p.sent[len(p.sent)-1].Text == text
	}, 15*time.Second, 25*time.Millisecond)
}

// spendMockRun pauses the run of the task as Loom does after the run spent its
// money allowance: the result holds the progress note of the agent.
func spendMockRun(t *testing.T, h *harness, id uuid.UUID) {
	t.Helper()
	current, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	for key, run := range h.peers.runs {
		if run.ID == current.RunID {
			run.ChatID, run.Status, run.Reason = inboxChat, "paused", "budget_exhausted"
			run.Result = "Two of three bookings are done."
			run.Version++
			h.peers.runs[key] = run
		}
	}
}

func TestSpentRunContinuesInSuccessorThroughChatInbox(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	var keys, texts []string
	loseReceipt := true
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/v1/chats/" + inboxChat + "/active-run":
			respond(w, loom.ChatRun{RunID: "successor", Status: "queued"})
		case "/v1/chats/" + inboxChat + "/messages":
			var body struct {
				Text string `json:"text"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			p.mu.Lock()
			defer p.mu.Unlock()
			keys, texts = append(keys, r.Header.Get("Idempotency-Key")), append(texts, body.Text)
			if _, replaced := p.runs["successor"]; !replaced {
				for key, run := range p.runs {
					run.Status, run.Reason = "cancelled", "superseded"
					p.runs[key] = run
				}
				p.runs["successor"] = loom.Run{
					ID: "successor", ChatID: inboxChat, AuthSessionID: "session", Status: "running", Version: 1,
					Interaction: loom.Interaction{ReplyMode: loom.ReplyModeAgent},
				}
			}
			if loseReceipt {
				loseReceipt = false
				disconnect(w)
				return true
			}
			respond(w, loom.MessageReceipt{
				Accepted: true, Delivered: true, MessageID: "message-1", RunID: "successor", Status: "queued",
			})
		default:
			return false
		}
		return true
	})
	h := setup(t, p)
	id := h.submit(t, 301)
	awaitLoomRun(t, h, id)
	spendMockRun(t, h, id)
	h.awaitReplies(t, 1)
	followup := h.submitText(t, 302, "Continue")
	h.await(t, followup, "completed")
	// The observer of the replaced run moves to its successor and gives its result.
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(t.Context(), id)
		return err == nil && current.RunID == "successor"
	}, 15*time.Second, 25*time.Millisecond)
	p.mu.Lock()
	successor := p.runs["successor"]
	successor.Status, successor.Result = "completed", "All three bookings are done."
	p.runs["successor"] = successor
	p.mu.Unlock()
	h.await(t, id, "completed")
	awaitLastReply(t, p, "All three bookings are done.")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[0].Text, "Two of three bookings are done.")
	require.Contains(t, p.sent[0].Text, "work allowance")
	require.Empty(t, p.inputs, "Input does not wake a run that spent its allowance")
	require.Equal(t, []string{followup.String(), followup.String()}, keys, "A lost receipt repeats the same message")
	require.Equal(t, texts[0], texts[1])
	require.Equal(t, "Continue", promptUserText(texts[0]))
}

func TestUndeliverableInboxMessageIsWithdrawnAndReported(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	var abandoned []string
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/v1/chats/" + inboxChat + "/messages":
			respond(w, loom.MessageReceipt{
				Accepted: true, MessageID: "message-1", Status: "blocked", Reason: "credits_exhausted",
			})
		case "/v1/chats/" + inboxChat + "/messages/message-1/abandon":
			p.mu.Lock()
			abandoned = append(abandoned, r.Header.Get("Idempotency-Key"))
			p.mu.Unlock()
			respond(w, loom.MessageReceipt{Accepted: true, Abandoned: true, MessageID: "message-1", Status: "abandoned"})
		default:
			return false
		}
		return true
	})
	h := setup(t, p)
	id := h.submit(t, 311)
	awaitLoomRun(t, h, id)
	spendMockRun(t, h, id)
	h.awaitReplies(t, 1)
	followup := h.submitText(t, 312, "Continue")
	h.await(t, followup, "failed")
	h.awaitReplies(t, 2)
	h.await(t, id, "awaiting_service")
	p.mu.Lock()
	defer p.mu.Unlock()
	// A retained message blocks all later messages of the chat.
	require.Equal(t, []string{followup.String() + ":abandon:message-1"}, abandoned)
	require.Contains(t, p.sent[1].Text, "budget is used")
	require.Empty(t, p.inputs)
}

func TestInboxWaitForLoomBlockerHasDeadline(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	abandoned := 0
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/v1/chats/" + inboxChat + "/messages":
			respond(w, loom.MessageReceipt{
				Accepted: true, MessageID: "message-1", Status: "recovery_pending", Reason: "cancellation_pending",
			})
		case "/v1/chats/" + inboxChat + "/messages/message-1/abandon":
			p.mu.Lock()
			abandoned++
			p.mu.Unlock()
			respond(w, loom.MessageReceipt{Accepted: true, Abandoned: true, MessageID: "message-1", Status: "abandoned"})
		default:
			return false
		}
		return true
	})
	h := setup(t, p)
	id := h.submit(t, 341)
	awaitLoomRun(t, h, id)
	spendMockRun(t, h, id)
	h.awaitReplies(t, 1)
	followup := h.submitText(t, 342, "Continue")
	h.await(t, followup, "awaiting_service")
	h.stop(t)
	p.mu.Lock()
	require.Zero(t, abandoned, "Loom can still stop the replaced run")
	p.mu.Unlock()
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=
	 jsonb_set(snapshot,'{chat_retry_until}','"2000-01-01T00:00:00Z"') WHERE id=$1`, followup)
	require.NoError(t, err)
	h.start(t)
	h.await(t, followup, "failed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, abandoned)
}

func TestControlAdoptsNewRoundWhenTargetRunEndedFirst(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/v1/chats/"+inboxChat+"/messages" {
			return false
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		// Another party cancelled the paused run before the message. Loom then starts
		// a new round, not a successor: the observer of the cancelled run does not follow it.
		if _, started := p.runs["new-round"]; !started {
			for key, run := range p.runs {
				run.Status, run.Reason = "cancelled", "client_cancel"
				p.runs[key] = run
			}
			p.runs["new-round"] = loom.Run{
				ID: "new-round", ChatID: inboxChat, AuthSessionID: "session", Status: "running", Version: 1,
				Interaction: loom.Interaction{ReplyMode: loom.ReplyModeAgent},
			}
		}
		respond(w, loom.MessageReceipt{
			Accepted: true, Delivered: true, MessageID: "message-1", RunID: "new-round", Status: "queued",
		})
		return true
	})
	h := setup(t, p)
	id := h.submit(t, 351)
	awaitLoomRun(t, h, id)
	spendMockRun(t, h, id)
	h.awaitReplies(t, 1)
	followup := h.submitText(t, 352, "Continue")
	h.await(t, id, "cancelled")
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(t.Context(), followup)
		return err == nil && current.RunID == "new-round" && current.Route == "new_run"
	}, 15*time.Second, 25*time.Millisecond)
	p.mu.Lock()
	run := p.runs["new-round"]
	run.Status, run.Result = "completed", "Answer of the new round"
	p.runs["new-round"] = run
	p.mu.Unlock()
	h.await(t, followup, "completed")
	awaitLastReply(t, p, "Answer of the new round")
}

func TestEmptyWalletPauseTellsCustomerOnce(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 361)
	awaitLoomRun(t, h, id)
	pauseMockRun(t, h, id, "step_limit")
	h.awaitReplies(t, 1)
	// The customer continued, and the woken run found the wallet empty.
	pauseMockRun(t, h, id, "credits_exhausted")
	h.awaitReplies(t, 2)
	h.stop(t)
	h.start(t)
	completeMockRun(p)
	h.await(t, id, "completed")
	h.awaitReplies(t, 3)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[1].Text, "budget is used")
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[1].ReplyMarkup, "Continue/Stop is obsolete")
	require.Equal(t, "Recovered original run", p.sent[2].Text)
}

// A lost abandonment response must not turn a text that Loom blocked for an
// unknown effect into a new request: the replay reports only "abandoned".
func TestUncertainEffectSurvivesLostAbandonmentResponse(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	abandoned := false
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch r.URL.Path {
		case "/v1/chats/" + inboxChat + "/messages":
			if abandoned {
				respond(w, loom.MessageReceipt{
					Accepted: true, Abandoned: true, MessageID: "message-1",
					Status: "abandoned", Reason: "explicit_withdrawal",
				})
				return true
			}
			respond(w, loom.MessageReceipt{
				Accepted: true, MessageID: "message-1", Status: "blocked", Reason: loom.ReasonEffectUncertain,
			})
		case "/v1/chats/" + inboxChat + "/messages/message-1/abandon":
			// Loom withdraws the message and an operator stops the run; the response is lost.
			abandoned = true
			for key, run := range p.runs {
				run.Status, run.Reason = "cancelled", "client_cancel"
				p.runs[key] = run
			}
			disconnect(w)
		default:
			return false
		}
		return true
	})
	h := setup(t, p)
	id := h.submit(t, 381)
	awaitLoomRun(t, h, id)
	spendMockRun(t, h, id)
	h.awaitReplies(t, 1)
	followup := h.submitText(t, 382, "Continue")
	h.await(t, id, "cancelled")
	h.await(t, followup, "failed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1, "The blocked text must not start a new run")
}

func TestSuccessorWithoutReceiptsEndsItsObserver(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 371)
	awaitLoomRun(t, h, id)
	// No input wakes this pause. The ended observer lets the next message release the chat.
	pauseMockRun(t, h, id, "recovery_context_too_large")
	h.await(t, id, "failed")
	h.awaitReplies(t, 1)
}

func TestBusyChatWithoutObserverIsReleasedForNewRun(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		reason      string
		leftover    bool
		reconciling bool
	}{
		"released":                {leftover: true},
		"uncertain_effect":        {reason: loom.ReasonEffectUncertain},
		"masked_uncertain_effect": {reason: loom.ReasonEffectUncertain, leftover: true},
		// The locator reports the expired session of the run before its unknown effect.
		"reconciling_behind_other_blocker": {reason: "authorization_required", reconciling: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testBusyChatWithoutObserver(t, tc.reason, tc.leftover, tc.reconciling)
		})
	}
}

// testBusyChatWithoutObserver ends the observer of a live run and sends a new
// request. With leftover set, the chat also holds an undelivered inbox message,
// and the locator reports it before the state of the run, as Loom does.
func testBusyChatWithoutObserver(t *testing.T, reason string, leftover, reconciling bool) {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	hidden := false
	var abandoned []string
	active := func() (loom.Run, bool) {
		for _, run := range p.runs {
			if run.Status == "running" || run.Status == "paused" || run.Status == "reconciling" {
				return run, true
			}
		}
		return loom.Run{}, false
	}
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		run, busy := active()
		_, created := p.runs[r.Header.Get("Idempotency-Key")]
		switch {
		case hidden && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/runs/"):
			rejectLoom(w, http.StatusNotFound, "not_found")
		case r.URL.Path == "/v1/runs" && r.Method == http.MethodPost && busy && !created:
			rejectLoom(w, http.StatusConflict, "chat_busy")
		case r.URL.Path == "/v1/chats/default":
			respond(w, map[string]string{"id": inboxChat})
		case r.URL.Path == "/v1/chats/"+inboxChat+"/messages/leftover/abandon":
			abandoned = append(abandoned, r.Header.Get("Idempotency-Key"))
			respond(w, loom.MessageReceipt{Accepted: true, Abandoned: true, MessageID: "leftover", Status: "abandoned"})
		case r.URL.Path == "/v1/chats/"+inboxChat+"/active-run" && busy && leftover && len(abandoned) == 0:
			respond(w, loom.ChatRun{
				RunID: run.ID, Status: "blocked", Reason: "earlier_message_pending", BlockingMessageID: "leftover",
			})
		case r.URL.Path == "/v1/chats/"+inboxChat+"/active-run" && busy:
			respond(w, loom.ChatRun{RunID: run.ID, Status: "blocked", Reason: reason})
		case r.URL.Path == "/v1/chats/"+inboxChat+"/active-run":
			respond(w, loom.ChatRun{Status: "terminal"})
		default:
			return false
		}
		return true
	})
	h := setup(t, p)
	// A typed rejection of the run read ends the observer; Loom still has the run.
	first := h.submit(t, 321)
	awaitLoomRun(t, h, first)
	orphan, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	hidden = true
	p.mu.Unlock()
	h.await(t, first, "failed")
	p.mu.Lock()
	hidden = false
	if reconciling {
		for key, run := range p.runs {
			run.Status = "reconciling"
			p.runs[key] = run
		}
	}
	p.mu.Unlock()
	second := h.submitText(t, 322, "Are you there?")
	if reason != "" {
		// Only an operator can reconcile an unknown effect: no cancellation, no new run.
		h.await(t, second, "failed")
	} else {
		awaitLoomRun(t, h, second)
		completeMockRun(p)
		h.await(t, second, "completed")
		awaitLastReply(t, p, "Recovered original run")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if leftover {
		require.Equal(t, []string{second.String() + ":release:leftover"}, abandoned)
	}
	if reason != "" {
		require.Empty(t, p.cancelKeys)
		require.Len(t, p.runs, 1)
		return
	}
	require.Equal(t, []string{second.String() + ":release:" + orphan.RunID}, p.cancelKeys)
}

func TestProxyNotFoundDoesNotEndObservedRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	outage, misses := false, 0
	serveLoom(t, p, func(w http.ResponseWriter, r *http.Request) bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		if !outage || r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/runs/") {
			return false
		}
		// A proxy without a selected backend answers for Loom.
		misses++
		http.NotFound(w, r)
		return true
	})
	h := setup(t, p)
	id := h.submit(t, 331)
	awaitLoomRun(t, h, id)
	p.mu.Lock()
	outage = true
	p.mu.Unlock()
	require.Eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return misses >= 2 },
		15*time.Second, 25*time.Millisecond)
	p.mu.Lock()
	outage = false
	p.mu.Unlock()
	completeMockRun(p)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, "Recovered original run", p.sent[0].Text)
}
