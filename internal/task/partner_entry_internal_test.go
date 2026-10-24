package task

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPartnerEntryPreservesAuthenticatedSenderAndDoesNotParseCaptions(t *testing.T) {
	t.Parallel()
	text := "/start partner_d3ddf61a79f94c5081f294e346d077ec"
	u := telegram.Update{ID: 1, Message: &telegram.Message{
		From: &telegram.User{ID: 7}, Chat: telegram.Chat{ID: 7, Type: "private"}, Text: text}}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	entry, err := normalize(u, raw)
	require.NoError(t, err)
	require.Equal(t, int64(7), entry.User.ID)
	require.Equal(t, text, entry.Text)
	require.Equal(t, kindMessage, entry.Kind)
	require.Empty(t, entry.StructuredCommand)
	require.Equal(t, uuid.MustParse("d3ddf61a-79f9-4c50-81f2-94e346d077ec"), *entry.PartnerCode)
	u.Message.Text, u.Message.Caption = "", text
	u.Message.Photo = []telegram.MediaFile{{FileID: "image", UniqueID: "unique", SizeBytes: 3, Width: 1, Height: 1}}
	u.Message.ID, u.Message.Date = 1, 123
	raw, err = json.Marshal(u)
	require.NoError(t, err)
	entry, err = normalize(u, raw)
	require.NoError(t, err)
	require.Nil(t, entry.PartnerCode)
	require.Equal(t, text, entry.Text)
	u.Message.Chat.Type = "group"
	raw, err = json.Marshal(u)
	require.NoError(t, err)
	_, err = normalize(u, raw)
	require.Error(t, err)
}
