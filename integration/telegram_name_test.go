package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
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

// Drive the owner API as Loom's tool caller; the mock provider does not infer intent.
func agentRequestsName(t *testing.T, h *harness, id uuid.UUID) (db.ProfileScope, db.UserProfile, string) {
	t.Helper()
	record, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	scope := ownerProfileScope(t, h, record.UserID)
	h.peers.mu.Lock()
	token := h.peers.token(scope.Subject)
	h.peers.mu.Unlock()
	code, raw := profileRequest(t, h, token, "request_name", uuid.New(), map[string]any{"expected_version": 0})
	require.Equal(t, 200, code, string(raw))
	var receipt profileAPIReceipt
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "succeeded", receipt.Status)
	require.NotNil(t, receipt.Result)
	return scope, *receipt.Result, token
}

func TestTelegramNameReplyTextAndMediaRemainAgentInputAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"Анна QA", "/cancel", "An unrelated picture"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.question = true
			h := setup(t, p)
			prompt := h.submitText(t, 91001, "/name")
			h.await(t, prompt, "awaiting_user")
			deliveredNamePrompt(t, h, prompt)
			scope, before, _ := agentRequestsName(t, h, prompt)
			require.NotNil(t, before.NameExpectation)
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
				require.Contains(
					t,
					p.inputs[0].Text,
					"Use zinusya_files.fetch with these IDs to import and inspect the attachment with Loom file tools; "+
						"metadata alone is not image content.",
				)
				require.NotContains(t, p.inputs[0].Text, "private-native-reply-file")
			}
		})
	}
}

func TestTelegramNameTextRequiresAgentToolAndPreservesUnrelatedRequests(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	prompt := h.submitText(t, 91101, "/name")
	h.await(t, prompt, "completed")
	deliveredNamePrompt(t, h, prompt)
	scope, before, token := agentRequestsName(t, h, prompt)
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
	require.Empty(t, final.FullName, "The command text alone must not mutate the name")
	require.Equal(t, before.NameExpectation, final.NameExpectation)
	code, raw := profileRequest(t, h, token, "complete_name", uuid.New(), map[string]any{
		"expected_version": final.Version, "expectation_id": final.NameExpectation.ID,
		"first_name": "Daniel", "last_name": "QA",
	})
	require.Equal(t, 200, code, string(raw))
	next := h.submitText(t, 91104, "Continue with my saved profile")
	h.await(t, next, "completed")
	h.awaitReplies(t, 4)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 4)
	observed := promptProfile(t, p.createdInputs[len(p.createdInputs)-1])
	require.Equal(t, "Daniel QA", observed.FullName)
	require.Nil(t, observed.NameExpectation)
}

func TestTelegramNameRequestSurvivesLoomOutageAndRestartWithoutNativeWrite(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	prompt := h.submitText(t, 91201, "/name Explicit QA")
	h.await(t, prompt, "awaiting_authorization")
	record, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	scope := ownerProfileScope(t, h, record.UserID)
	before, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Empty(t, before.FullName)
	require.Nil(t, before.NameExpectation)
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, prompt, "completed")
	after, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version)
	require.Empty(t, after.FullName)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], "/name Explicit QA")
}

func TestTelegramNameDialogueCarriesUntrustedHintsAndPlainAgentSuggestion(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "suggested-name", Label: "Anna Maria de la Cruz"}}
	h := setup(t, p)
	update := telegram.Update{ID: 91301, Message: &telegram.Message{
		ID:   91301,
		From: &telegram.User{ID: 456, FirstName: "Anna Maria", LastName: "de la Cruz", Username: "anna_dance"},
		Chat: telegram.Chat{ID: 456, Type: "private"},
		Text: "/name",
	}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "awaiting_user")
	require.Equal(t, "Anna Maria de la Cruz", replyChoice(t, p, h, accepted.ID))
	scope := ownerProfileScope(t, h, accepted.UserID)
	profile, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Empty(t, profile.FullName, "Presenting a suggestion must not save it")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], `"needs_real_name":true`)
	require.Contains(t, p.createdInputs[0], `"telegram_first_name":"Anna Maria"`)
	require.Contains(t, p.createdInputs[0], `"telegram_last_name":"de la Cruz"`)
	require.Contains(t, p.createdInputs[0], `"telegram_username":"anna_dance"`)
	require.Contains(t, p.createdInputs[0], "Never save Telegram hints without")
}
