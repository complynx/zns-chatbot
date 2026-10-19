package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func nameReply(t *testing.T, h *harness, id int64, text string) uuid.UUID {
	t.Helper()
	messageID := int64(1)
	h.peers.mu.Lock()
	var quoted string
	if messageID > 0 && messageID <= int64(len(h.peers.sent)) {
		quoted = h.peers.sent[messageID-1].Text
	}
	h.peers.mu.Unlock()
	update := telegram.Update{
		ID: id,
		Message: &telegram.Message{
			ID:   id,
			From: &telegram.User{ID: 456, LanguageCode: "en"},
			Chat: telegram.Chat{ID: 456, Type: "private"},
			Text: text,
			ReplyToMessage: &telegram.Message{
				Text: quoted,
				ID:   messageID,
				From: &telegram.User{ID: 123, IsBot: true},
				Chat: telegram.Chat{ID: 456, Type: "private"},
			},
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	return accepted.ID
}

func deliveredNamePrompt(t *testing.T, h *harness, id uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		out, err := h.store.ListOutbox(t.Context(), id)
		return err == nil && len(out) > 0 && out[len(out)-1].State == "delivered"
	}, 5*time.Second, 5*time.Millisecond)
}

func TestTelegramNameReplyTextAndMediaRemainAgentInputAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"Анна QA", "/cancel", "An unrelated picture"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			h := setup(t, p)
			prompt := h.submitText(t, 91001, "/name")
			h.await(t, prompt, "completed")
			deliveredNamePrompt(t, h, prompt)
			record, err := h.store.GetTask(t.Context(), prompt)
			require.NoError(t, err)
			scope := ownerProfileScope(t, h, record.UserID)
			before, err := h.store.GetProfile(t.Context(), scope)
			require.NoError(t, err)
			require.NotNil(t, before.NameExpectation)
			p.mu.Lock()
			p.question = true
			p.mu.Unlock()
			active := h.submitText(t, 91002, "Please help me")
			h.await(t, active, "awaiting_user")
			h.stop(t)
			h.start(t)
			var reply uuid.UUID
			if text == "An unrelated picture" {
				update := telegram.Update{
					ID: 91003,
					Message: &telegram.Message{
						ID:      91003,
						Date:    time.Now().Unix(),
						From:    &telegram.User{ID: 456},
						Chat:    telegram.Chat{ID: 456, Type: "private"},
						Caption: text,
						Document: &telegram.MediaFile{
							FileID:   "private-native-reply-file",
							UniqueID: "native-reply-fixture",
							Filename: "picture.pdf",
							MIMEType: "application/pdf",
						},
						ReplyToMessage: &telegram.Message{ID: 1},
					},
				}
				raw, e := json.Marshal(update)
				require.NoError(t, e)
				accepted, _, e := h.service.Accept(t.Context(), update, raw)
				require.NoError(t, e)
				reply = accepted.ID
			} else {
				reply = nameReply(t, h, 91003, text)
				require.Equal(t, reply, nameReply(t, h, 91003, text))
			}
			h.await(t, reply, "completed")
			after, err := h.store.GetProfile(t.Context(), scope)
			require.NoError(t, err)
			require.Equal(t, before.Version, after.Version)
			require.Equal(t, before.FullName, after.FullName)
			require.Equal(
				t,
				before.NameExpectation,
				after.NameExpectation,
				"A reply reference must never authorize a name write",
			)
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Len(t, p.inputs, 1)
			require.Equal(t, "steering", p.inputs[0].Kind)
			require.Contains(t, p.inputs[0].Text, text)
			require.NotNil(t, p.inputs[0].Presentation)
			require.Empty(t, p.cancelKeys)
			if text == "An unrelated picture" {
				require.Contains(t, p.inputs[0].Text, "Use zinusya_files tools with these IDs")
				require.NotContains(t, p.inputs[0].Text, "private-native-reply-file")
			}
		})
	}
}

func TestTelegramExplicitNameClosesSharedExpectationButFreeTextDoesNot(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	prompt := h.submitText(t, 91101, "/name")
	h.await(t, prompt, "completed")
	deliveredNamePrompt(t, h, prompt)
	task, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	scope := ownerProfileScope(t, h, task.UserID)
	before, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	ordinary := h.submitText(t, 91102, "Tell me a cat joke")
	h.await(t, ordinary, "completed")
	h.awaitReplies(t, 2)
	unchanged, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, before.Version, unchanged.Version)
	require.Equal(t, before.NameExpectation, unchanged.NameExpectation)
	explicit := h.submitText(t, 91103, "/name Daniel QA")
	h.await(t, explicit, "completed")
	h.awaitReplies(t, 3)
	final, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, "Daniel QA", final.FullName)
	require.Nil(t, final.NameExpectation)
	next := h.submitText(t, 91104, "Continue with my saved profile")
	h.await(t, next, "completed")
	h.awaitReplies(t, 4)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 2)
	require.Contains(t, p.createdInputs[len(p.createdInputs)-1], "profile_version")
	require.Contains(t, p.createdInputs[len(p.createdInputs)-1], "Daniel QA")
}

func TestTelegramNameUncertainPromptReplyWaitsForAgentWithoutChangingName(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown, p.loseSend = true, true
	h := setup(t, p)
	prompt := h.submitText(t, 91201, "/name")
	h.await(t, prompt, "delivery_unknown")
	record, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	scope := ownerProfileScope(t, h, record.UserID)
	before, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	reply := nameReply(t, h, 91202, "Not a name, cancel please")
	h.await(t, reply, "awaiting_authorization")
	after, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version)
	require.Equal(t, before.FullName, after.FullName)
	require.Equal(t, before.NameExpectation, after.NameExpectation)
	explicit := h.submitText(t, 91203, "/name Explicit QA")
	h.await(t, explicit, "completed")
	after, err = h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, "Explicit QA", after.FullName)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.await(t, reply, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], "Not a name, cancel please")
}
