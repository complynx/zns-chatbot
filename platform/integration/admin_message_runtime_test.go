package integration_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminMessageRuntimePreviewSendResults(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, locale)
			require.NoError(t, err)
			handle(t, f.b, message(900, 202, `/send_message_to 101 --msg "Synthetic runtime message"`))
			require.Empty(t, chatMessages(t, f, 101), "preview must not send")
			var id int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT id FROM core.admin_messages WHERE actor='bob'`).Scan(&id),
			)
			cards := chatMessages(t, f, 202)
			require.NotEmpty(t, cards)
			assert.Contains(t, cards[len(cards)-1].Text, "Synthetic runtime message")
			before := f.model.calls
			handle(t, f.b, message(901, 202, "What is two plus two?"))
			assert.Greater(t, f.model.calls, before, "draft never traps free text")
			callback := message(902, 202, "")
			callback.Message = nil
			callback.Callback = &telegram.Callback{
				ID:      "902",
				From:    telegram.User{ID: 202},
				Message: telegram.Message{Chat: telegram.Chat{ID: 202, Type: "private"}},
				Data:    fmt.Sprintf("adminmsg:send:%d", id),
			}
			handle(t, f.b, callback)
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			sent := chatMessages(t, f, 101)
			require.Len(t, sent, 1)
			assert.Equal(t, "Synthetic runtime message", sent[0].Text)
			result, err := (adminmessage.Service{DB: f.db}).Results(t.Context(), "bob", id)
			require.NoError(t, err)
			require.Len(t, result, 1)
			assert.Equal(t, "sent", result[0].State)
			callback.ID = 903
			callback.Callback.Data = fmt.Sprintf("adminmsg:results:%d", id)
			handle(t, f.b, callback)
			cards = chatMessages(t, f, 202)
			if locale == "ru" {
				assert.Contains(t, cards[len(cards)-1].Text, "отправлено")
			} else {
				assert.Contains(t, cards[len(cards)-1].Text, "sent")
			}
		})
	}
}

func TestAdminMessageRuntimeCommandValidation(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"quoted",
		`/send_message_to '[101,"@Channel:4",101]' --html '<b>Hello</b>'`,
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		[]adminmessage.Destination{{Chat: "101"}, {Chat: "@channel", Thread: 4}},
		preview.Request.Destinations,
	)
	assert.Equal(t, "HTML", preview.Request.Content.ParseMode)
	for index, command := range []string{`/send_message_to 101 --msg "unterminated`, `/send_message_to 0 --msg x`, `/send_message_to @abcde:0 --msg x`} {
		_, err = service.PreviewCommand(t.Context(), "bob", fmt.Sprintf("invalid-%d", index), command)
		requireCode(t, err, "admin_message_invalid")
	}
	_, err = service.PreviewCommand(t.Context(), "alice", "forbidden", `/send_message_to 101 --msg x`)
	requireCode(t, err, "forbidden")
}
