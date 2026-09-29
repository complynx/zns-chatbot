package stickerassets_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/stickerassets"
	"github.com/complynx/zns-chatbot/platform/internal/stickercache"
	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type cache struct {
	values map[stickercache.Key]string
	hits   int
}

func (c *cache) Get(ctx context.Context, key stickercache.Key, process stickercache.Processor) (string, error) {
	c.hits++
	if value, exists := c.values[key]; exists {
		return value, nil
	}
	value, err := process(ctx, key)
	if err == nil {
		c.values[key] = value
	}
	return value, err
}

type decoder struct {
	formats []stickermedia.Format
	err     error
}

func (d *decoder) Normalize(_ context.Context, body []byte, format stickermedia.Format) ([]stickermedia.Frame, error) {
	d.formats = append(d.formats, format)
	if string(body) != "artwork" {
		return nil, errors.New("unexpected artwork")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		return nil, err
	}
	return []stickermedia.Frame{{PNG: encoded.Bytes(), Width: 2, Height: 2}}, d.err
}

type model struct {
	inputs []agent.Input
}

func (m *model) Plan(_ context.Context, input agent.Input) (agent.Plan, error) {
	m.inputs = append(m.inputs, input)
	return agent.Plan{View: "media", Text: "A red circle."}, nil
}

func fixture(t *testing.T, sticker telegram.Sticker) (stickerassets.Service, *atomic.Int32, *decoder, *model) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/bottest/getCustomEmojiStickers":
			// An unrelated first result catches accidental array-position matching.
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []telegram.Sticker{
				{FileID: "wrong", CustomEmojiID: "999"}, sticker,
			}}))
		case "/bottest/getFile":
			assert.NoError(
				t,
				json.NewEncoder(w).
					Encode(map[string]any{"ok": true, "result": telegram.File{Path: "artwork", Size: 7}}),
			)
		case "/file/bottest/artwork":
			_, err := w.Write([]byte("artwork"))
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	d, m := &decoder{}, &model{}
	return stickerassets.Service{Telegram: telegram.Client{Base: server.URL, Token: "test"},
		Cache: &cache{values: make(map[stickercache.Key]string)}, Decoder: d, Model: m}, &calls, d, m
}

func TestCustomEmojiUsesActualArtworkAndReusesCanonicalDescription(t *testing.T) {
	t.Parallel()
	service, calls, decoded, planned := fixture(
		t,
		telegram.Sticker{FileID: "file", CustomEmojiID: "123", IsAnimated: true},
	)
	message := telegram.Message{Text: "🙂🙂", Entities: []telegram.MessageEntity{
		{Type: "custom_emoji", Offset: 0, Length: 2, CustomEmojiID: "123"},
		{Type: "custom_emoji", Offset: 2, Length: 2, CustomEmojiID: "123"},
	}}
	result, err := service.Describe(t.Context(), message)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "described", result.Items[0].Status)
	assert.Equal(t, "A red circle.", result.Items[0].Description)
	assert.Len(t, result.Items[0].Occurrences, 2)
	assert.Equal(t, []stickermedia.Format{stickermedia.TGS}, decoded.formats)
	require.Len(t, planned.inputs, 1)
	assert.Empty(t, planned.inputs[0].Text)
	assert.Empty(t, planned.inputs[0].History)
	assert.Equal(t, "custom_emoji", planned.inputs[0].AssetTask.Kind)
	assert.EqualValues(t, 3, calls.Load())
	message.Text += " private caption for a different user"
	_, err = service.Describe(t.Context(), message)
	require.NoError(t, err)
	assert.EqualValues(t, 3, calls.Load(), "cache hit must avoid Telegram download and model/decoder work")
	assert.Len(t, planned.inputs, 1)
	assert.Equal(t, 2, service.Cache.(*cache).hits, "one popularity bump per unique asset per message")
}

func TestStickerFormatAndFailureAreExplicit(t *testing.T) {
	t.Parallel()
	for name, sticker := range map[string]telegram.Sticker{
		"webp": {FileID: "file", UniqueID: "stable"},
		"webm": {FileID: "file", UniqueID: "stable", IsVideo: true},
		"tgs":  {FileID: "file", UniqueID: "stable", IsAnimated: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service, _, decoded, planned := fixture(t, sticker)
			decoded.err = stickermedia.ErrInvalid
			message := telegram.Message{Sticker: &sticker}
			result, err := service.Describe(t.Context(), message)
			require.NoError(t, err)
			assert.Equal(t, "unavailable", result.Items[0].Status)
			assert.Empty(t, result.Items[0].Description)
			assert.Empty(t, planned.inputs)
			assert.Empty(t, service.Cache.(*cache).values, "failed observations must not enter the shared cache")
			decoded.err = nil
			result, err = service.Describe(t.Context(), message)
			require.NoError(t, err)
			assert.Equal(t, "described", result.Items[0].Status)
			assert.Equal(
				t,
				[]stickermedia.Format{stickermedia.Format(name), stickermedia.Format(name)},
				decoded.formats,
			)
		})
	}
}

func TestCustomEmojiBudgetAndMalformedOffsets(t *testing.T) {
	t.Parallel()
	service := stickerassets.Service{}
	message := telegram.Message{Text: strings.Repeat("x", stickerassets.MaxAssets+2)}
	for index := range stickerassets.MaxAssets + 2 {
		message.Entities = append(message.Entities, telegram.MessageEntity{
			Type: "custom_emoji", Offset: index, Length: 1, CustomEmojiID: strconv.Itoa(index + 1),
		})
	}
	result, err := service.Describe(t.Context(), message)
	require.NoError(t, err)
	assert.Len(t, result.Items, stickerassets.MaxAssets)
	assert.Equal(t, 2, result.Omitted)
	assert.Equal(t, "unavailable", result.Items[0].Status)
	message.Text = "🙂"
	message.Entities = []telegram.MessageEntity{{Type: "custom_emoji", Offset: 1, Length: 1, CustomEmojiID: "1"}}
	_, err = service.Describe(t.Context(), message)
	require.ErrorIs(t, err, telegram.ErrCustomEmojiEntity)
}

func TestCaptionEmojiAndCancellation(t *testing.T) {
	t.Parallel()
	service := stickerassets.Service{}
	message := telegram.Message{Caption: "x", CaptionEntities: []telegram.MessageEntity{
		{Type: "custom_emoji", Offset: 0, Length: 1, CustomEmojiID: "1"},
	}}
	result, err := service.Describe(t.Context(), message)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "x", result.Items[0].Occurrences[0].Placeholder)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Describe(ctx, message)
	require.ErrorIs(t, err, context.Canceled)
}
