// Package symboldescribe runs the separate, checkpointed visual-description procedure.
package symboldescribe

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"

	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type Telegram interface {
	CustomEmojiStickers(context.Context, []string) ([]telegram.Sticker, error)
	StickerSet(context.Context, string) (telegram.StickerSet, error)
	ReadFile(context.Context, string, int64) ([]byte, error)
}

type media struct {
	raw  []byte
	mime string
}

const sheetColumns = 10
const sheetTile = 128
const sheetPadding = 8
const previewBackground = 96
const previewAlpha = 255
const maxSetPreviewBytes = 30 * 1024 * 1024

func target(ctx context.Context, tg Telegram, ref symbols.Reference) (telegram.Sticker, error) {
	if ref.Kind == symbols.Sticker {
		s := telegram.Sticker{FileID: ref.FileID, UniqueID: ref.ID, SetName: ref.SetName,
			IsAnimated: ref.Animated, IsVideo: ref.Video}
		if ref.ThumbnailID != "" {
			s.Thumbnail = &telegram.MediaFile{FileID: ref.ThumbnailID}
		}
		return s, nil
	}
	items, err := tg.CustomEmojiStickers(ctx, []string{ref.ID})
	if err != nil {
		return telegram.Sticker{}, err
	}
	if len(items) != 1 || items[0].CustomEmojiID != ref.ID {
		return telegram.Sticker{}, symbols.ErrInvalid
	}
	return items[0], nil
}

func visual(ctx context.Context, tg Telegram, s telegram.Sticker) (media, error) {
	raw, err := tg.ReadFile(ctx, s.FileID, files.MaxReadBytes)
	if err != nil {
		return media{}, err
	}
	mime := http.DetectContentType(raw)
	switch {
	case s.IsAnimated:
		mime = "application/x-tgsticker"
	case s.IsVideo:
		mime = "video/webm"
	case mime == "image/png", mime == "image/jpeg", mime == "image/webp", mime == "image/gif":
		// Preserve original bytes: a raster container can itself contain animation.
	default:
		return media{}, errors.New("unsupported sticker media container")
	}
	return media{raw, mime}, nil
}

// A contact sheet retains the whole bounded set in Telegram order. It is context,
// not a claim that arbitrary neighboring items are a known composite.
func contactSheet(ctx context.Context, tg Telegram, set telegram.StickerSet) (media, error) {
	const tile = sheetTile
	const columns = sheetColumns
	if len(set.Stickers) == 0 || len(set.Stickers) > 200 {
		return media{}, symbols.ErrInvalid
	}
	canvas := previewCanvas(image.Rect(0, 0, columns*tile, ((len(set.Stickers)+columns-1)/columns)*tile))
	total := 0
	for n, s := range set.Stickers {
		id := s.FileID
		if s.Thumbnail != nil {
			id = s.Thumbnail.FileID
		} else if s.IsAnimated || s.IsVideo {
			return media{}, errors.New("set member preview unavailable")
		}
		raw, err := tg.ReadFile(ctx, id, files.MaxReadBytes)
		if err != nil {
			return media{}, err
		}
		total += len(raw)
		if total > maxSetPreviewBytes {
			return media{}, errors.New("set previews exceed total download limit")
		}
		img, err := decodeImage(raw)
		if err != nil {
			return media{}, err
		}
		thumb := imaging.Fit(img, tile-sheetPadding, tile-sheetPadding, imaging.Lanczos)
		at := image.Pt((n%columns)*tile+(tile-thumb.Bounds().Dx())/2,
			(n/columns)*tile+(tile-thumb.Bounds().Dy())/2)
		draw.Draw(canvas, image.Rectangle{Min: at, Max: at.Add(thumb.Bounds().Size())}, thumb, image.Point{}, draw.Over)
	}
	var out bytes.Buffer
	err := png.Encode(&out, canvas)
	return media{out.Bytes(), "image/png"}, err
}

func previewCanvas(bounds image.Rectangle) *image.RGBA {
	canvas := image.NewRGBA(bounds)
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{
		R: previewBackground, G: previewBackground, B: previewBackground, A: previewAlpha,
	}), image.Point{}, draw.Src)
	return canvas
}

func decodeImage(raw []byte) (image.Image, error) {
	r := bytes.NewReader(raw)
	config, err := imageConfig(raw)
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
		return nil, errors.New("symbol preview dimensions exceed limit")
	}
	if len(raw) > 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return webp.Decode(r)
	}
	if bytes.HasPrefix(raw, []byte("\x89PNG")) {
		return png.Decode(r)
	}
	return jpeg.Decode(r)
}

func imageConfig(raw []byte) (image.Config, error) {
	r := bytes.NewReader(raw)
	if len(raw) > 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return webp.DecodeConfig(r)
	}
	if bytes.HasPrefix(raw, []byte("\x89PNG")) {
		return png.DecodeConfig(r)
	}
	return jpeg.DecodeConfig(r)
}
