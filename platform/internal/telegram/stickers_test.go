package telegram_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestCustomEmojiUTF16Spans(t *testing.T) {
	t.Parallel()
	text := "\U0001f680x\U0001f469\u200d\U0001f4bb \u2764\ufe0f"
	entities := []telegram.MessageEntity{
		{Type: "bold", Offset: 0, Length: 11},
		{Type: "custom_emoji", Offset: 9, Length: 2, CustomEmojiID: "42"},
		{Type: "custom_emoji", Offset: 3, Length: 5, CustomEmojiID: "42"},
	}
	spans, err := telegram.CustomEmojiSpans(text, entities)
	require.NoError(t, err)
	assert.Equal(t, []telegram.CustomEmojiSpan{
		{ID: "42", Offset: 9, Length: 2, Text: "\u2764\ufe0f"},
		{ID: "42", Offset: 3, Length: 5, Text: "\U0001f469\u200d\U0001f4bb"},
	}, spans)
	spans, err = telegram.CustomEmojiSpans(text, nil)
	require.NoError(t, err)
	assert.Empty(t, spans, "ordinary Unicode emoji do not require sticker lookup")
}

func TestMalformedCustomEmojiSpans(t *testing.T) {
	t.Parallel()
	for _, entity := range []telegram.MessageEntity{
		{Offset: 1, Length: 1}, {Offset: 0, Length: 1}, {Offset: -1, Length: 1},
		{Offset: 0, Length: 0}, {Offset: 0, Length: 4}, {Offset: 3, Length: 1},
		{Offset: 0, Length: int(^uint(0) >> 1)},
	} {
		entity.Type, entity.CustomEmojiID = "custom_emoji", "42"
		_, err := telegram.CustomEmojiSpans("\U0001f680x", []telegram.MessageEntity{entity})
		require.ErrorIs(t, err, telegram.ErrCustomEmojiEntity)
	}
	entity := telegram.MessageEntity{Type: "custom_emoji", Offset: 0, Length: 2, CustomEmojiID: "42"}
	_, err := telegram.CustomEmojiSpans("\U0001f680", []telegram.MessageEntity{entity, entity})
	require.ErrorIs(t, err, telegram.ErrCustomEmojiEntity)
	entity.CustomEmojiID = ""
	_, err = telegram.CustomEmojiSpans("\U0001f680", []telegram.MessageEntity{entity})
	require.ErrorIs(t, err, telegram.ErrCustomEmojiEntity)
	_, err = telegram.CustomEmojiSpans(string([]byte{0xff}), nil)
	require.ErrorIs(t, err, telegram.ErrCustomEmojiEntity)
}

func TestCustomEmojiBounds(t *testing.T) {
	t.Parallel()
	entities := make([]telegram.MessageEntity, telegram.MaxCustomEmoji+1)
	for i := range entities {
		entities[i] = telegram.MessageEntity{Type: "custom_emoji", Offset: i, Length: 1, CustomEmojiID: "42"}
	}
	text := strings.Repeat("x", len(entities))
	spans, err := telegram.CustomEmojiSpans(text, entities[:telegram.MaxCustomEmoji])
	require.NoError(t, err)
	assert.Len(t, spans, telegram.MaxCustomEmoji)
	_, err = telegram.CustomEmojiSpans(text, entities)
	require.ErrorIs(t, err, telegram.ErrCustomEmojiLimit)
	_, err = telegram.CustomEmojiSpans(text, make([]telegram.MessageEntity, 1001))
	require.ErrorIs(t, err, telegram.ErrCustomEmojiEntity)
	client := telegram.Client{}
	_, err = client.GetCustomEmojiStickers(t.Context(), make([]string, telegram.MaxCustomEmoji+1))
	require.ErrorIs(t, err, telegram.ErrCustomEmojiLimit)
	_, err = client.GetCustomEmojiStickers(t.Context(), []string{""})
	require.ErrorIs(t, err, telegram.ErrCustomEmojiID)
	stickers, err := client.GetCustomEmojiStickers(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, stickers)
}

func TestCustomEmojiStickerTransport(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bottest/getCustomEmojiStickers", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		var input struct {
			IDs []string `json:"custom_emoji_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, []string{"1", "2", "3"}, input.IDs)
		_, err := fmt.Fprint(w, `{"ok":true,"result":[
   {"file_id":"video","file_unique_id":"v","type":"custom_emoji","custom_emoji_id":"3","width":100,"height":100,"is_video":true,"is_animated":false,"file_size":100},
   {"file_id":"static","file_unique_id":"s","type":"custom_emoji","custom_emoji_id":"1","width":100,"height":100,"is_video":false,"is_animated":false,"thumbnail":{"file_id":"thumb","width":10,"height":10}},
   {"file_id":"animated","file_unique_id":"a","type":"custom_emoji","custom_emoji_id":"2","width":100,"height":100,"is_video":false,"is_animated":true}
  ]}`)
		assert.NoError(t, err)
	}))
	defer server.Close()
	client := telegram.Client{Base: server.URL, Token: "test", HTTP: server.Client()}
	stickers, err := client.GetCustomEmojiStickers(t.Context(), []string{"1", "2", "3"})
	require.NoError(t, err)
	require.Len(t, stickers, 3)
	assert.Equal(t, "3", stickers[0].CustomEmojiID)
	assert.True(t, stickers[0].IsVideo)
	assert.Equal(t, int64(100), stickers[0].Size)
	assert.Equal(t, "s", stickers[1].UniqueID)
	require.NotNil(t, stickers[1].Thumbnail)
	assert.Equal(t, "thumb", stickers[1].Thumbnail.FileID)
	assert.False(t, stickers[1].IsAnimated)
	assert.True(t, stickers[2].IsAnimated)
}

func TestStickerMessageDecode(t *testing.T) {
	t.Parallel()
	var message telegram.Message
	err := json.Unmarshal(
		[]byte(
			`{"text":"x","entities":[{"type":"custom_emoji","offset":0,"length":1,"custom_emoji_id":"1"}],"caption":"y","caption_entities":[{"type":"custom_emoji","offset":0,"length":1,"custom_emoji_id":"2"}],"sticker":{"file_id":"real-media","file_unique_id":"unique","type":"regular","width":512,"height":512,"is_video":false,"is_animated":true,"emoji":"alternative"}}`,
		),
		&message,
	)
	require.NoError(t, err)
	require.NotNil(t, message.Sticker)
	assert.Equal(t, "real-media", message.Sticker.FileID)
	assert.True(t, message.Sticker.IsAnimated)
	spans, err := telegram.CustomEmojiSpans(message.Caption, message.CaptionEntities)
	require.NoError(t, err)
	assert.Equal(t, []telegram.CustomEmojiSpan{{ID: "2", Offset: 0, Length: 1, Text: "y"}}, spans)
	spans, err = telegram.CustomEmojiSpans(message.Text, message.Entities)
	require.NoError(t, err)
	assert.Equal(t, []telegram.CustomEmojiSpan{{ID: "1", Offset: 0, Length: 1, Text: "x"}}, spans)
}
