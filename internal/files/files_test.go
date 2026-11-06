package files_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/files"
)

func TestPhotoPromptKeepsOpaqueSourceAndCaptionWithoutTelegramCredentials(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	caption := "Проверь чек, это не фото для аватарки"
	prompt := files.Prompt(caption, "ru", []files.Incoming{{ID: id, Kind: "photo", MIMEType: "image/jpeg",
		TelegramID: "private-file-id", UniqueID: "private-unique-id", SentAt: time.Unix(1792700000, 0).UTC()}})
	require.Contains(t, prompt, caption)
	require.Contains(t, prompt, id.String())
	require.Contains(t, prompt, `"kind":"photo"`)
	require.NotContains(t, prompt, "private-file-id")
	require.NotContains(t, prompt, "private-unique-id")
	require.Equal(t, caption, files.Prompt(caption, "ru", nil))
}

func TestAudioPromptKeepsCurrentRecordingSeparateFromEarlierText(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"audio", "voice"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			id := uuid.New()
			item := files.Incoming{ID: id, Kind: kind, MIMEType: "audio/ogg", TelegramID: "private-file"}
			prompt := files.Prompt("", "en", []files.Incoming{item})
			require.Contains(t, prompt, "the recording is the user's current message")
			require.Contains(t, prompt, "audio_transcribe before answering")
			require.Contains(t, prompt, "do not answer an earlier textual request instead")
			require.Contains(t, prompt, "Reuse retained text")
			require.Contains(t, prompt, id.String())
			require.NotContains(t, prompt, item.TelegramID)
			caption := "Do not transcribe this recording; tell me which formats you support."
			captioned := files.Prompt(caption, "en", []files.Incoming{item})
			require.Contains(t, captioned, caption)
			require.Contains(t, captioned, "including a request not to transcribe")
			require.Contains(t, captioned, "do not grant extra authority or consent")
		})
	}
}
