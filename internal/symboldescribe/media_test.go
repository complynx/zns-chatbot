package symboldescribe_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

type originalMediaTelegram struct {
	symboldescribe.Telegram

	raw   []byte
	reads []string
}

func (f *originalMediaTelegram) ReadFile(_ context.Context, id string, _ int64) ([]byte, error) {
	f.reads = append(f.reads, id)
	return f.raw, nil
}

func TestAnimatedStickerPreservesOriginalInsteadOfThumbnail(t *testing.T) {
	t.Parallel()
	var original bytes.Buffer
	w := gzip.NewWriter(&original)
	_, err := w.Write([]byte(`{"w":64,"h":64,"fr":1,"ip":0,"op":2,"layers":[]}`))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	tg := &originalMediaTelegram{raw: original.Bytes()}
	lc := uploadOriginal(t, tg, symbols.Reference{Kind: symbols.Sticker, ID: "unique",
		FileID: "original", Animated: true, ThumbnailID: "preview"})
	require.Equal(t, []string{"original"}, tg.reads)
	require.Equal(t, []string{"application/x-tgsticker"}, lc.uploadMIMEs)
	require.Equal(t, original.Bytes(), lc.uploadBytes[0])
}

func TestAnimatedRasterIsNotFlattened(t *testing.T) {
	t.Parallel()
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	second.SetColorIndex(0, 0, 1)
	var original bytes.Buffer
	require.NoError(t, gif.EncodeAll(&original, &gif.GIF{
		Image: []*image.Paletted{first, second}, Delay: []int{100, 100},
	}))
	tg := &originalMediaTelegram{raw: original.Bytes()}
	lc := uploadOriginal(t, tg, symbols.Reference{Kind: symbols.Sticker, ID: "unique", FileID: "original"})
	require.Equal(t, []string{"image/gif"}, lc.uploadMIMEs)
	require.Equal(t, original.Bytes(), lc.uploadBytes[0])
	decoded, err := gif.DecodeAll(bytes.NewReader(lc.uploadBytes[0]))
	require.NoError(t, err)
	require.Len(t, decoded.Image, 2)
}

func uploadOriginal(t *testing.T, tg *originalMediaTelegram, ref symbols.Reference) *fakeLoom {
	t.Helper()
	lc := &fakeLoom{status: "running"}
	flow := symboldescribe.Flow{Telegram: tg, Loom: lc}
	state := symboldescribe.State{Generation: uuid.New(), Reference: ref}
	require.NoError(t, flow.Step(t.Context(), loom.Auth{}, &state, func(symboldescribe.State) error { return nil }))
	require.Len(t, lc.uploadBytes, 1)
	return lc
}
