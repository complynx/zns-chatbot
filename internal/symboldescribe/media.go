// Package symboldescribe runs the separate, checkpointed visual-description procedure.
package symboldescribe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"time"

	"github.com/disintegration/imaging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	raw, err := readVisualFile(ctx, tg, s.FileID)
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
		descriptorLog(ctx).DebugContext(ctx, "prepare set preview", "index", n+1, "set_size", len(set.Stickers))
		id := s.FileID
		if s.Thumbnail != nil {
			id = s.Thumbnail.FileID
		} else if s.IsAnimated || s.IsVideo {
			return media{}, errors.New("set member preview unavailable")
		}
		raw, err := readVisualFile(ctx, tg, id)
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

// Only Telegram file reads are retried. A transient preview fetch must not
// immediately strand an acknowledged original upload. Loom effects are never replayed here.
func readVisualFile(ctx context.Context, tg Telegram, id string) ([]byte, error) {
	ctx, span := trace.SpanFromContext(ctx).TracerProvider().Tracer("shuttle-symbols").Start(ctx, "symbols.read_file")
	defer span.End()
	const attempts = 3
	const retryDelay = 250 * time.Millisecond
	for attempt := range attempts {
		started := time.Now()
		raw, err := tg.ReadFile(ctx, id, files.MaxReadBytes)
		status, outcome := 0, ""
		if upstream, ok := errors.AsType[*telegram.Error](err); ok {
			status, outcome = upstream.StatusCode, string(upstream.Outcome)
		}
		span.AddEvent("read_attempt", trace.WithAttributes(attribute.Int("attempt", attempt+1),
			attribute.Int("http.response.status_code", status), attribute.Bool("success", err == nil),
			attribute.Int("bytes", len(raw))))
		descriptorLog(ctx).DebugContext(ctx, "read visual file", "file_key", fmt.Sprintf("%x", sha256.Sum256([]byte(id))),
			"attempt", attempt+1, "elapsed_ms", time.Since(started).Milliseconds(), "bytes", len(raw),
			"success", err == nil, "status", status, "outcome", outcome, "retryable", retryableFileRead(err))
		if err == nil || attempt == attempts-1 || !retryableFileRead(err) {
			if err != nil {
				span.SetStatus(codes.Error, "visual file read failed")
			}
			return raw, err
		}
		delay := time.Duration(attempt+1) * retryDelay
		if upstream, ok := errors.AsType[*telegram.Error](err); ok && upstream.RetryAfter > delay {
			delay = upstream.RetryAfter
		}
		if delay > 2*time.Second {
			span.SetStatus(codes.Error, "visual file backoff exceeds bound")
			return nil, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			span.SetStatus(codes.Error, "visual file read cancelled")
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable file read")
}

type descriptorLogKey struct{}

func descriptorLog(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(descriptorLogKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func retryableFileRead(err error) bool {
	if errors.Is(err, telegram.ErrFileUnavailable) {
		return true
	}
	upstream, ok := errors.AsType[*telegram.Error](err)
	return ok && (upstream.Retryable || upstream.StatusCode >= 500 && upstream.StatusCode <= 599)
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
