package task

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestNativePhotoRoutingKeepsCaptionAndReplyIntent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, caption, documentMIME string
		reply, want                 bool
	}{
		{name: "captionless photo", want: true},
		{name: "payment caption", caption: "Чек оплаты"},
		{name: "different purpose", caption: "Найди расписание на скриншоте"},
		{name: "reply to question", reply: true},
		{name: "JPEG document", documentMIME: "image/jpeg", want: true},
		{name: "PNG document", documentMIME: "image/png", want: true},
		{name: "PDF document", documentMIME: "application/pdf"},
		{name: "captioned image document", documentMIME: "image/jpeg", caption: "Чек оплаты"},
		{name: "image document replying to question", documentMIME: "image/png", reply: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			media := telegram.MediaFile{FileID: "private-photo", UniqueID: "private-unique"}
			m := &telegram.Message{ID: 1, Date: time.Now().Unix(), From: &telegram.User{ID: 7},
				Chat: telegram.Chat{ID: 7, Type: "private"}, Caption: tc.caption, Photo: []telegram.MediaFile{media}}
			if tc.reply {
				m.ReplyToMessage = &telegram.Message{ID: 2}
			}
			if tc.documentMIME != "" {
				m.Photo = nil
				media.MIMEType = tc.documentMIME
				m.Document = &media
			}
			update := telegram.Update{ID: 1, Message: m}
			raw, err := json.Marshal(update)
			require.NoError(t, err)
			i, err := normalize(update, raw)
			require.NoError(t, err)
			require.Equal(t, kindMessage, i.Kind)
			require.Equal(t, tc.want, i.NativeAvatar)
			require.Equal(t, tc.caption, i.Text)
		})
	}
}
