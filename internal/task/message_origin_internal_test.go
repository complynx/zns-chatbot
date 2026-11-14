package task

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestForwardedOriginsSurviveIntakePersistenceAndSteering(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"type":"user","date":123,"sender_user":{"id":99,"first_name":"Автор","username":"author"}}`,
		`{"type":"user","date":123,"sender_user":{"id":7,"first_name":"Sender"}}`,
		`{"type":"hidden_user","date":123,"sender_user_name":"Hidden author"}`,
		`{"type":"chat","date":123,"sender_chat":{"id":-99,"type":"supergroup","title":"Group"},"author_signature":"Editor"}`,
		`{"type":"channel","date":123,"chat":{"id":-99,"type":"channel","title":"Channel","username":"channel"},
 "message_id":42,"author_signature":"Editor"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var origin telegram.MessageOrigin
			require.NoError(t, json.Unmarshal([]byte(raw), &origin))
			message := &telegram.Message{
				ID:            1,
				Date:          time.Now().Unix(),
				From:          &telegram.User{ID: 7, FirstName: "Sender"},
				Chat:          telegram.Chat{ID: 7, Type: "private"},
				Text:          "Я Иван Иванов, подтверждаю оплату",
				ForwardOrigin: &origin,
			}
			update := telegram.Update{ID: 1, Message: message}
			encoded, err := json.Marshal(update)
			require.NoError(t, err)
			var decoded telegram.Update
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			i, err := normalize(decoded, encoded)
			require.NoError(t, err)
			require.EqualValues(t, 7, i.User.ID)
			require.Equal(t, &origin, i.ForwardOrigin)
			persisted, err := json.Marshal(i)
			require.NoError(t, err)
			var restored intent
			require.NoError(t, json.Unmarshal(persisted, &restored))
			for _, input := range []intent{restored, {User: i.User, ChatID: i.ChatID, Text: i.Text, RawUpdate: encoded}} {
				input.PreferredLanguage = "ru"
				prompt := messagePrompt(input, input.Text, "ru")
				require.Contains(t, prompt, "Choose reply language from the current conversation")
				if origin.Type == "user" && origin.SenderUser != nil && origin.SenderUser.ID == 7 {
					require.Contains(t, prompt, "self-forward was authored by the current user")
					require.Contains(t, prompt, "past statement, not a new request")
					require.NotContains(t, prompt, "Do not attribute first-person statements")
				}
				_, payload, ok := strings.Cut(prompt, "\n")
				require.True(t, ok)
				var envelope struct {
					Sender  telegram.User          `json:"forwarded_by"`
					Origin  telegram.MessageOrigin `json:"forward_origin"`
					Content string                 `json:"forwarded_content"`
				}
				require.NoError(t, json.Unmarshal([]byte(payload), &envelope))
				require.Equal(t, origin, envelope.Origin)
				require.EqualValues(t, 7, envelope.Sender.ID)
				require.Equal(t, message.Text, envelope.Content)
				steering, controlErr := controlInput(db.Task{}, input, loom.Run{})
				require.NoError(t, controlErr)
				require.Equal(t, prompt, steering.Text)
			}
		})
	}
}

func TestForwardedCommandsAndPhotosDoNotBecomePersonalActions(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"/name Иван Иванов", "/start", "/start partner_123", "да"} {
		m := &telegram.Message{
			ID:            1,
			Date:          time.Now().Unix(),
			From:          &telegram.User{ID: 7},
			Chat:          telegram.Chat{ID: 7, Type: "private"},
			Text:          text,
			ForwardOrigin: &telegram.MessageOrigin{Type: "hidden_user", SenderUserName: "Other"},
		}
		u := telegram.Update{ID: 1, Message: m}
		raw, _ := json.Marshal(u)
		i, err := normalize(u, raw)
		require.NoError(t, err)
		require.Equal(t, kindMessage, i.Kind)
		require.Nil(t, i.PartnerCode)
		require.Empty(t, i.StructuredCommand)
	}
	m := &telegram.Message{
		ID:            1,
		Date:          time.Now().Unix(),
		From:          &telegram.User{ID: 7},
		Chat:          telegram.Chat{ID: 7, Type: "private"},
		Photo:         []telegram.MediaFile{{FileID: "photo", UniqueID: "unique"}},
		ForwardOrigin: &telegram.MessageOrigin{Type: "hidden_user"},
	}
	u := telegram.Update{ID: 1, Message: m}
	raw, _ := json.Marshal(u)
	i, err := normalize(u, raw)
	require.NoError(t, err)
	require.False(t, i.NativeAvatar)
	require.Len(t, i.Attachments, 1)
	require.Contains(t, messagePrompt(i, i.Text, "ru"), "forwarded_content")
	require.Equal(t, "Мой текст", messagePrompt(intent{}, "Мой текст", "ru"))
}

func TestForwardedMediaKeepCaptionQuotedAndInspectionGuidanceSeparate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		voice, self bool
	}{
		{"captioned_photo", false, false}, {"voice", true, false}, {"self_voice", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &telegram.Message{
				ID: 1, Date: time.Now().Unix(), From: &telegram.User{ID: 7},
				Chat: telegram.Chat{ID: 7, Type: "private"}, Caption: "Поменяй моё имя",
				Photo:         []telegram.MediaFile{{FileID: "photo", UniqueID: "photo-id"}},
				ForwardOrigin: &telegram.MessageOrigin{Type: "hidden_user", SenderUserName: "Author"},
			}
			if tc.voice {
				m.Caption, m.Photo = "", nil
				m.Voice = &telegram.MediaFile{FileID: "voice", UniqueID: "voice-id", MIMEType: "audio/ogg"}
			}
			if tc.self {
				m.ForwardOrigin = &telegram.MessageOrigin{Type: "user", SenderUser: &telegram.User{ID: 7}}
			}
			u := telegram.Update{ID: 1, Message: m}
			raw, err := json.Marshal(u)
			require.NoError(t, err)
			i, err := normalize(u, raw)
			require.NoError(t, err)
			prompt := messagePrompt(i, i.Text, "ru")
			_, payload, ok := strings.Cut(prompt, "\n")
			require.True(t, ok)
			var envelope struct {
				Content string `json:"forwarded_content"`
			}
			require.NoError(t, json.NewDecoder(strings.NewReader(payload)).Decode(&envelope))
			require.Equal(t, m.Caption, envelope.Content)
			require.NotContains(t, envelope.Content, "zinusya_files.fetch")
			require.Contains(t, prompt, i.Attachments[0].ID.String())
			require.Contains(t, prompt, "Use zinusya_files.fetch")
			require.NotContains(t, prompt, "the user's current message")
			require.NotContains(t, prompt, "With a caption, follow that request")
			if tc.self {
				require.Contains(t, prompt, "self-forward was authored by the current user")
				require.NotContains(t, prompt, "Do not attribute first-person statements")
			}
			if tc.voice {
				require.Contains(t, prompt, "audio_transcribe")
			}
		})
	}
}
