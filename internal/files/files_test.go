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
