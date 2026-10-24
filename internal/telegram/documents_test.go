package telegram_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestSendDocumentUploadsPrivateBytesAndChecksReceipt(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bot123:secret/sendDocument", r.URL.Path)
		if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
		assert.Equal(t, "42", r.FormValue("chat_id"))
		assert.JSONEq(
			t,
			"{\"inline_keyboard\":[[{\"text\":\"Crop\",\"web_app\":{\"url\":\"https://example.t" +
"est/app/#avatar-crop?draft=00000000-0000-0000-0000-000000000001\"}}]]}",
			r.FormValue("reply_markup"),
		)
		file, header, err := r.FormFile("document")
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		assert.NoError(t, err)
		assert.Equal(t, "zinusya-avatar.jpg", header.Filename)
		assert.Equal(t, []byte("private JPEG fixture"), body)
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":7,"chat":{"id":42}}}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	receipt, err := client.SendDocument(context.Background(), telegram.SendDocumentRequest{
		ChatID: 42, Filename: "zinusya-avatar.jpg", Content: []byte("private JPEG fixture"),
		ReplyMarkup: &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{
			Text: "Crop", WebApp: &telegram.WebAppInfo{
				URL: "https://example.test/app/#avatar-crop?draft=00000000-0000-0000-0000-000000000001"},
		}}}},
	})
	require.NoError(t, err)
	require.Equal(t, telegram.Receipt{MessageID: 7, ChatID: 42}, receipt)
}

func TestDocumentLostOrMismatchedReceiptIsUnknown(t *testing.T) {
	t.Parallel()
	for _, lost := range []bool{true, false} {
		t.Run(strconv.FormatBool(lost), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if lost {
					connection, _, err := w.(http.Hijacker).Hijack()
					if assert.NoError(t, err) {
						assert.NoError(t, connection.Close())
					}
					return
				}
				fmt.Fprint(w, `{"ok":true,"result":{"message_id":7,"chat":{"id":99}}}`)
			}))
			defer server.Close()
			client, err := telegram.New(server.URL, "123:secret", server.Client())
			require.NoError(t, err)
			_, err = client.SendDocument(context.Background(), telegram.SendDocumentRequest{
				ChatID: 42, Filename: "avatar.jpg", Content: []byte("fixture"),
			})
			var failure *telegram.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, telegram.Unknown, failure.Outcome)
			require.False(t, failure.Retryable)
			require.Equal(t, int32(1), calls.Load())
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
