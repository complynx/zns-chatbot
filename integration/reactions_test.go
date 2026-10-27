package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

type reactionCall struct {
	ChatID    int64 `json:"chat_id"`
	MessageID int64 `json:"message_id"`
	Reaction  []struct {
		Type  string `json:"type"`
		Emoji string `json:"emoji"`
	} `json:"reaction"`
}

func TestOptionalReactionCanReplaceAndRemoveWithoutAnotherTextReply(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.reactionsEnabled, p.steering = true, true
	h := setup(t, p)
	registrationUser(t, h, 456, false)
	id := h.submitText(t, 9851, "Thanks for fixing that")
	h.await(t, id, "running")
	waitCalls := func(count int) {
		t.Helper()
		require.Eventually(t, func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return len(p.reactionCalls) == count
		}, 8*time.Second, 20*time.Millisecond)
	}
	waitCalls(1)
	for index, emoji := range []string{"🤝", ""} {
		seq := int64(index + 3)
		p.mu.Lock()
		p.reactionEvents[0].ReactionsSeq = seq
		p.reactionEvents[0].Reactions["assistant"] = nil
		if emoji != "" {
			p.reactionEvents[0].Reactions["assistant"] = []string{emoji}
		}
		p.reactionEvents = append(p.reactionEvents, loom.ChatEvent{Seq: seq, Type: "message.reaction",
			Data: json.RawMessage(`{"message_seq":1,"actor":"assistant"}`)})
		p.mu.Unlock()
		waitCalls(index + 2)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, "🤝", p.reactionCalls[1].Reaction[0].Emoji)
	require.Empty(t, p.reactionCalls[2].Reaction)
}

func TestReactionRechecksLocalRevocationAfterTokenRefresh(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	user, _ := registrationUser(t, h, 456, false)
	id := h.submitText(t, 9852, "A completed ordinary answer")
	h.await(t, id, "completed")
	u, err := h.store.GetUser(t.Context(), user)
	require.NoError(t, err)
	p.mu.Lock()
	p.reactionsEnabled = true
	p.tokenHook = func(subject string) {
		if subject == u.Subject {
			_, updateErr := h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", user)
			require.NoError(t, updateErr)
		}
	}
	p.mu.Unlock()
	h.iam.Invalidate(u.Subject)
	c := db.ReactionCursor{UserID: user, TaskID: id, LoomChatID: "local-revocation"}
	require.NoError(t, h.store.RegisterReactionChat(t.Context(), user, id, c.LoomChatID))
	require.NoError(t, h.store.ReactionPage(t.Context(), c, 2, false, []db.ReactionProjection{
		{MessageSeq: 1, Watermark: 2, ChatID: 456, MessageID: 9852, Emoji: "🙈"},
	}))
	require.Eventually(t, func() bool {
		var state string
		readErr := h.store.Pool().QueryRow(t.Context(), "SELECT state FROM shuttle_telegram_reactions").Scan(&state)
		return readErr == nil && state == "failed"
	}, 8*time.Second, 20*time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.reactionCalls)
}

// Both handlers run under peers.mu, like the other HTTP fixtures.
func (p *peers) reactionHistory(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var events []loom.ChatEvent
	for _, e := range p.reactionEvents {
		if e.Seq > after && len(events) < limit {
			events = append(events, e)
		}
	}
	respond(w, map[string]any{"events": events})
}

func (p *peers) telegramReaction(w http.ResponseWriter, r *http.Request) {
	var v reactionCall
	if !p.reactionsEnabled || json.NewDecoder(r.Body).Decode(&v) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	p.reactionCalls = append(p.reactionCalls, v)
	if p.loseReaction {
		p.loseReaction = false
		disconnect(w)
		return
	}
	respond(w, map[string]any{"ok": true, "result": true})
}

func TestOptionalReactionUsesExactIncomingMessageWithoutBlockingAnswer(t *testing.T) {
	t.Parallel()
	for _, unknown := range []bool{false, true} {
		t.Run(strconv.FormatBool(unknown), func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.reactionsEnabled, p.loseReaction = true, unknown
			h := setup(t, p)
			registrationUser(t, h, 456, false)
			id := h.submitText(t, 9850, "You got my name wrong")
			h.await(t, id, "completed")
			require.Eventually(t, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return len(p.reactionCalls) == 1
			}, 8*time.Second, 20*time.Millisecond)
			var state string
			want := "delivered"
			if unknown {
				want = "unknown"
			}
			require.Eventually(t, func() bool {
				err := h.store.Pool().QueryRow(t.Context(), "SELECT state FROM shuttle_telegram_reactions").Scan(&state)
				return err == nil && state == want
			}, 3*time.Second, 20*time.Millisecond)
			p.mu.Lock()
			call := p.reactionCalls[0]
			answers := len(p.sent)
			p.mu.Unlock()
			require.EqualValues(t, 456, call.ChatID)
			require.EqualValues(t, 9850, call.MessageID)
			require.Len(t, call.Reaction, 1)
			require.Equal(t, "🙈", call.Reaction[0].Emoji)
			require.Positive(t, answers)
			if unknown {
				h.stop(t)
				h.start(t)
				// Recovery must leave the uncertain attempt frozen.
				require.NoError(t, h.store.RecoverUnknownReactions(t.Context()))
				var count int
				require.NoError(t, h.store.Pool().QueryRow(t.Context(),
					"SELECT count(*) FROM shuttle_telegram_reactions WHERE state='pending'").Scan(&count))
				require.Zero(t, count)
			}
		})
	}
}
