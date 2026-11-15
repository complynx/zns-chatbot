package task

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPairNotificationLinksInviter(t *testing.T) {
	t.Parallel()
	var notice db.PairInvitationDelivery
	notice.Kind = pairNoticePending
	notice.Context.Timezone = "UTC"
	notice.Context.InviterName = "🕺 Даниил [literal]"
	notice.Context.InviterUserID = uuid.New()
	notice.Context.Invitation.EventID = uuid.New()
	notice.Context.EventName = "Zouk Weekend"
	notice.Context.Invitation.ExpiresAt = time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	input, err := pairNotificationContent(notice, "ru", telegram.User{ID: 7, FirstName: "Даниил"})
	require.NoError(t, err)
	require.NotContains(t, input.Text, notice.Context.InviterUserID.String())
	require.Contains(t, input.Text, notice.Context.InviterName)
	require.Contains(t, input.Text, notice.Context.EventName)
	require.NotContains(t, input.Text, notice.Context.Invitation.EventID.String())
	require.Len(t, input.Entities, 1)
	require.EqualValues(t, 7, input.Entities[0].User.ID)
	require.Equal(t, 0, input.Entities[0].Offset)
	require.NoError(t, render.Validate(render.Message{Text: input.Text, Entities: input.Entities}))
	revoked := db.User{BotID: 1, Organization: "org", TelegramUserID: 7, Revoked: true}
	mention := pairInviterMention(revoked, db.EventScope{BotID: 1, Organization: "org"}, notice.Context.InviterName)
	withoutLink, err := pairNotificationContent(notice, "ru", mention)
	require.NoError(t, err)
	require.Equal(t, input.Text, withoutLink.Text, "An unavailable mention must not suppress the notification")
	require.Empty(t, withoutLink.Entities)
}

func TestBusinessNotificationRetriesExplicitEntityRejectionAsPlainText(t *testing.T) {
	t.Parallel()
	requests := make(chan telegram.SendMessageRequest, 2)
	var calls atomic.Int32
	s := menuHTTPService(t, func(w http.ResponseWriter, r *http.Request) {
		var request telegram.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- request
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":10,"chat":{"id":7}}}`)
	})
	finished := 0
	err := s.sendBusinessNotification(t.Context(), telegram.SendMessageRequest{
		ChatID: 7, Text: "Inviter invites you",
		Entities: []telegram.MessageEntity{{Type: "text_mention", Offset: 0, Length: 7,
			User: &telegram.User{ID: 8, FirstName: "Inviter"}}},
	}, "pair_notification", func(_ context.Context, outcome string, raw json.RawMessage, delay time.Duration) error {
		finished++
		require.Equal(t, deliveryDelivered, outcome)
		require.JSONEq(t, `{"message_id":10,"chat_id":7}`, string(raw))
		require.Zero(t, delay)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, finished, "Persist only the final send outcome")
	first, fallback := <-requests, <-requests
	require.Len(t, first.Entities, 1)
	require.Empty(t, fallback.Entities)
	require.Equal(t, first.Text, fallback.Text)
	require.Equal(t, first.ChatID, fallback.ChatID)
}
