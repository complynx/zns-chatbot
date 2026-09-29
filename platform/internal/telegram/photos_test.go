package telegram_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestPhotoDocumentChoosesBoundedVariant(t *testing.T) {
	t.Parallel()
	variants := []telegram.PhotoSize{
		{FileID: "large", Width: 1200, Height: 1600},
		{FileID: "too-big", Width: 4000, Height: 6000, Size: telegram.MaxDocumentBytes + 1},
		{FileID: "bad-dimensions", Width: -1, Height: -1},
		{FileID: "extreme", Width: 2147483647, Height: 2147483647},
		{FileID: "small", Width: 100, Height: 100, Size: 1000},
	}
	file, err := telegram.PhotoDocument(variants)
	require.NoError(t, err)
	assert.Equal(t, "large", file.FileID)
	assert.Zero(t, file.Size, "unknown size must still be checked during download")
	_, err = telegram.PhotoDocument(variants[1:4])
	require.ErrorIs(t, err, telegram.ErrInvalidDocument)
	_, err = telegram.PhotoDocument(nil)
	require.ErrorIs(t, err, telegram.ErrInvalidDocument)
}
