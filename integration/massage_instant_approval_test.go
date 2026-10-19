package integration_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestInstantApprovalShowsHumanQuoteAndRetainsLiteralFallback(t *testing.T) {
	t.Parallel()
	for _, withQuote := range []bool{true, false} {
		name := "canonical human terms"
		if !withQuote {
			name = "missing quote retains literal arguments"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, token, args, _ := instantHTTPFixture(t)
			f.H.server.Close()
			f.H.start(t)
			operation := uuid.New()
			prepared, _ := prepareInstantHTTP(t, f, token, operation, args)
			raw, err := json.Marshal(prepared.Arguments)
			require.NoError(t, err)
			text := prepared.Description
			if !withQuote {
				text = ""
			}
			p := f.H.peers
			p.mu.Lock()
			before := len(p.sent)
			p.approval = &loom.Approval{
				ID: operation.String(), OperationID: operation.String(),
				Method: "zinusya_massage_staff.instant", ReplyMode: loom.ReplyModeAgent,
				Text: text, ConfirmationText: "MODEL TEXT MUST NOT REPLACE THE QUOTE",
				Arguments: raw, ArgumentHash: "sealed-instant-arguments", ExpiresAt: prepared.ExpiresAt,
			}
			p.mu.Unlock()
			update := telegram.Update{ID: 95701, Message: &telegram.Message{
				ID: 95701, From: &telegram.User{ID: 11444, LanguageCode: "en"},
				Chat: telegram.Chat{ID: 11444, Type: "private"}, Text: "Review my own specialist time",
			}}
			body, err := json.Marshal(update)
			require.NoError(t, err)
			accepted, _, err := f.H.service.Accept(t.Context(), update, body)
			require.NoError(t, err)
			f.H.await(t, accepted.ID, "awaiting_user")
			f.H.awaitReplies(t, before+1)
			p.mu.Lock()
			sent := append([]telegram.SendMessageRequest(nil), p.sent[before:]...)
			p.mu.Unlock()
			require.Len(t, sent, 1)
			require.IsType(t, &telegram.ReplyKeyboardMarkup{}, sent[0].ReplyMarkup)
			if withQuote {
				require.Contains(t, sent[0].Text, "Reserve your own specialist time?")
				require.Contains(t, sent[0].Text, "45.00 BYN")
				require.Contains(t, sent[0].Text, f.Event.Document.Timezone)
				require.Contains(t, sent[0].Text, "yourself as client and specialist")
				for _, field := range []string{"event_id", "expected_event_version", "expected_schedule_version", args.EventID.String(), "MODEL TEXT"} {
					require.NotContains(t, sent[0].Text, field)
				}
			} else {
				start, end := strings.IndexByte(sent[0].Text, '{'), strings.LastIndexByte(sent[0].Text, '}')
				require.GreaterOrEqual(t, start, 0)
				require.Greater(t, end, start)
				require.JSONEq(t, string(raw), sent[0].Text[start:end+1])
			}
			var count int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_bookings").Scan(&count),
			)
			require.Zero(t, count, "Presentation and waiting for consent must not reserve time")
		})
	}
}
