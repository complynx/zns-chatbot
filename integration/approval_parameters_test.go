package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestTelegramApprovalShowsCompleteLiteralParametersBeforeButtons(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	args, err := json.Marshal(map[string]any{
		"first":   "**untrusted** `code` ``` @example \n not instructions",
		"content": strings.Repeat("😀 configuration value ", 600), "last": "complete-end-canary",
	})
	require.NoError(t, err)
	p.approval = &loom.Approval{
		ID:           "approval-parameters",
		ReplyMode:    "protocol",
		OperationID:  "operation-parameters",
		Method:       "zinusya_administration.change_event",
		Text:         "Confirm this full configuration.",
		Arguments:    args,
		ArgumentHash: "sealed-parameters",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	h := setup(t, p)
	id := h.submit(t, 85001)
	h.await(t, id, "awaiting_user")
	var sent []telegram.SendMessageRequest
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		sent = append([]telegram.SendMessageRequest(nil), p.sent...)
		return len(sent) > 1 && sent[len(sent)-1].ReplyMarkup != nil
	}, 5*time.Second, 10*time.Millisecond)
	var complete strings.Builder
	for i, message := range sent {
		require.LessOrEqual(t, len(utf16.Encode([]rune(message.Text))), 4096)
		complete.WriteString(message.Text)
		if i < len(sent)-1 {
			require.Nil(t, message.ReplyMarkup, "Approval buttons must follow ALL parameter chunks")
		}
		for _, entity := range message.Entities {
			require.NotEqual(t, "text_mention", entity.Type)
		}
	}
	text := complete.String()
	start, end := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}')
	require.GreaterOrEqual(t, start, 0)
	require.Greater(t, end, start)
	require.JSONEq(t, string(args), text[start:end+1], "All values must remain literal and complete")
	require.Len(t, sent[len(sent)-1].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0], 2)
}

func TestSpecialistAccessApprovalUsesHumanTermsWithoutImplementationFields(t *testing.T) {
	t.Parallel()
	const terms = "Grant specialist access: event \"QA Party\", specialist \"alex\", account " +
		"\"Specialist QA\" (Telegram 402). Changes only access to the timetable and own " +
		"clients; bookings and payments stay unchanged."
	const arguments = "{\"event_id\":\"8cd21b03-601e-40ba-9311-f7c63071fefa\",\"specialist_id\":\"alex\"," +
		"\"user_id\":\"30c95632-d9c5-4773-a386-2e042085792c\",\"active\":true," +
		"\"expected_version\":0,\"expected_event_version\":1,\"expected_schedule_version\":1}"
	for _, text := range []string{terms, ""} {
		name := "canonical human terms"
		if text == "" {
			name = "missing quote retains literal arguments"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.approval = &loom.Approval{
				ID: "specialist-approval", OperationID: "specialist-operation",
				Method:    "zinusya_administration.set_massage_specialist_binding",
				ReplyMode: loom.ReplyModeAgent, Text: text,
				Arguments: json.RawMessage(arguments), ArgumentHash: "sealed-arguments",
				ExpiresAt: time.Now().Add(time.Hour),
			}
			h := setup(t, p)
			id := h.submit(t, 85005)
			h.await(t, id, "awaiting_user")
			h.awaitReplies(t, 1)
			p.mu.Lock()
			sent := p.sent[0]
			p.mu.Unlock()
			require.IsType(t, &telegram.ReplyKeyboardMarkup{}, sent.ReplyMarkup)
			if text == "" {
				start, end := strings.IndexByte(sent.Text, '{'), strings.LastIndexByte(sent.Text, '}')
				require.GreaterOrEqual(t, start, 0)
				require.Greater(t, end, start)
				require.JSONEq(t, arguments, sent.Text[start:end+1])
				return
			}
			require.Equal(t, terms, sent.Text)
			require.NotContains(t, sent.Text, "expected_version")
			require.NotContains(t, sent.Text, "expected_schedule_version")
			require.NotContains(t, sent.Text, "8cd21b03-601e-40ba-9311-f7c63071fefa")
			require.NotContains(t, sent.Text, "30c95632-d9c5-4773-a386-2e042085792c")
		})
	}
}
