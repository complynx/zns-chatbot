package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func submitSymbol(t *testing.T, h *harness, id int64, sticker bool) uuid.UUID {
	t.Helper()
	m := telegram.Message{ID: id, Date: time.Now().Unix(), From: &telegram.User{ID: 456, LanguageCode: "ru"},
		Chat: telegram.Chat{ID: 456, Type: "private"}}
	if sticker {
		m.Sticker = &telegram.Sticker{FileID: "private-sticker", UniqueID: "sticker-key", Emoji: "😂"}
	} else {
		m.Text = "🦴🦴"
		m.Entities = []telegram.MessageEntity{
			{Type: "custom_emoji", Offset: 0, Length: 2, CustomEmojiID: "10"},
			{Type: "custom_emoji", Offset: 2, Length: 2, CustomEmojiID: "11"},
		}
	}
	u := telegram.Update{ID: id, Message: &m}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	task, _, err := h.service.Accept(context.Background(), u, raw)
	require.NoError(t, err)
	return task.ID
}

func TestSymbolIntakeCreatesActiveReactionAndSteersWaitingRun(t *testing.T) {
	t.Parallel()
	for _, waiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "waiting"}[waiting], func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.question = waiting
			h := setup(t, p)
			registrationUser(t, h, 456, false)
			if waiting {
				initial := h.submit(t, 9800)
				h.await(t, initial, "awaiting_user")
			}
			id := submitSymbol(t, h, 9801, true)
			h.await(t, id, "completed")
			p.mu.Lock()
			var text string
			inputCount := len(p.inputs)
			if waiting && inputCount > 0 {
				text = p.inputs[0].Text
			} else if !waiting {
				text = p.createdInputs[0]
			}
			p.mu.Unlock()
			if waiting {
				require.Equal(t, 1, inputCount)
			}
			require.Contains(t, text, `"event":"sticker_reaction"`)
			require.Contains(t, text, "current active user message")
			require.Contains(t, text, "not a photo/video analysis request")
			require.NotContains(t, text, "private-sticker")
			require.NotContains(t, text, "😂")
			require.NotContains(t, text, "Attachment metadata")
		})
	}
}

func TestCustomEmojiDescriptionsPreserveCompositionInAgentInput(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	registrationUser(t, h, 456, false)
	id := submitSymbol(t, h, 9810, false)
	h.await(t, id, "awaiting_authorization")
	for _, key := range []string{"10", "11"} {
		r, err := h.store.PendingSymbol(t.Context(), 123, symbols.Emoji, key)
		require.NoError(t, err)
		d := symbols.Description{Text: "Bone end", Category: "fragment", Composition: "Joins the other end"}
		require.NoError(t, h.store.CompleteSymbolDescription(t.Context(), 123, r, d, r.Workflow))
	}
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.await(t, id, "completed")
	p.mu.Lock()
	text := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, text, "</custom-emoji><custom-emoji>")
	require.Contains(t, text, "Bone end")
	require.NotContains(t, text, "🦴")
	var count int
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_symbols").Scan(&count))
	require.Equal(t, 2, count)
}

func TestKnownStickerDescriptionRemainsInsideReactionEvent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	registrationUser(t, h, 456, false)
	id := submitSymbol(t, h, 9820, true)
	h.await(t, id, "awaiting_authorization")
	r, err := h.store.PendingSymbol(t.Context(), 123, symbols.Sticker, "sticker-key")
	require.NoError(t, err)
	d := symbols.Description{Text: "A cat hiding its face", Category: "emotion", Reaction: "Possible embarrassment"}
	require.NoError(t, h.store.CompleteSymbolDescription(t.Context(), 123, r, d, r.Workflow))
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.await(t, id, "completed")
	p.mu.Lock()
	text := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, text, `"event":"sticker_reaction"`)
	require.Contains(t, text, `"description":"A cat hiding its face"`)
	require.Contains(t, text, `"reaction_hint":"Possible embarrassment"`)
	require.Contains(t, text, "not a photo/video analysis request")
	require.NotContains(t, text, "Attachment metadata")
}

func TestRestrictedRunWaitsForCapabilityWithoutUnrestrictedFallback(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.outputValidationUnavailable = true
	h := setup(t, p)
	registrationUser(t, h, 456, false)
	id := submitSymbol(t, h, 9830, true)
	h.await(t, id, "awaiting_service")
	p.mu.Lock()
	before := len(p.createKeys)
	p.outputValidationUnavailable = false
	p.mu.Unlock()
	require.Zero(t, before)
	h.await(t, id, "completed")
	p.mu.Lock()
	policies := append([]*loom.OutputPolicy(nil), p.createdOutputPolicies...)
	p.mu.Unlock()
	require.Len(t, policies, 1)
	require.NotNil(t, policies[0])
	require.True(t, policies[0].DisallowCustomEmoji)
}
