package telegram_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestSelectAVPreservesTelegramKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []telegram.AVKind{telegram.AudioKind, telegram.VoiceKind, telegram.VideoKind, telegram.VideoNoteKind} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			var message telegram.Message
			require.NoError(
				t,
				json.Unmarshal(
					[]byte(
						`{"`+string(
							kind,
						)+`":{"file_id":"opaque","file_unique_id":"stable","duration":241,"file_size":123},"caption":"context"}`,
					),
					&message,
				),
			)
			attachment, present, err := telegram.SelectAV(message)
			require.NoError(t, err)
			require.True(t, present)
			assert.Equal(t, kind, attachment.Kind)
			assert.Equal(t, "opaque", attachment.Document.FileID)
			assert.Equal(t, "stable", attachment.Document.UniqueID)
			assert.Equal(t, 241, attachment.Duration, "metadata does not decide decoded duration acceptance")
		})
	}
}

func TestSelectAVRejectsAmbiguityAndIgnoresOtherMedia(t *testing.T) {
	t.Parallel()
	_, present, err := telegram.SelectAV(
		telegram.Message{Document: &telegram.Document{MIME: "video/mp4"}, Sticker: &telegram.Sticker{IsVideo: true}},
	)
	require.NoError(t, err)
	assert.False(t, present)
	for _, message := range []telegram.Message{
		{Audio: &telegram.Audio{FileID: "a"}, Voice: &telegram.Voice{FileID: "b"}},
		{Video: &telegram.Video{}},
		{Voice: &telegram.Voice{FileID: "a", Size: -1}},
		{VideoNote: &telegram.VideoNote{FileID: "a", Size: telegram.MaxDocumentBytes + 1}},
	} {
		_, present, err = telegram.SelectAV(message)
		require.ErrorIs(t, err, telegram.ErrAVAttachment)
		assert.True(t, present)
	}
}
