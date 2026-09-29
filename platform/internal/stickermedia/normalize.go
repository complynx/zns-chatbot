// Package stickermedia normalizes Telegram sticker assets inside the isolated decoder.
package stickermedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Format string

const (
	WebP             Format = "webp"
	TGS              Format = "tgs"
	WebM             Format = "webm"
	MaxInputBytes           = 1 << 20
	maxJSONBytes            = 2 << 20
	maxFrameBytes           = 2 << 20
	maxSide                 = 512
	frameCount              = 4
	bytesPerPixel           = 4
	maxProbeBytes           = 256 << 10
	maxJSONDepth            = 64
	imageLayerType          = 2
	operationTimeout        = 15 * time.Second
)

var ErrInvalid = errors.New("invalid or unsupported sticker asset")
var ErrLimit = errors.New("sticker resource limit exceeded")

type Frame struct {
	PNG       []byte        `json:"png"`
	Timestamp time.Duration `json:"timestamp_ns"`
	Width     int           `json:"width"`
	Height    int           `json:"height"`
}

// Normalizer paths are trusted deployment configuration, never request fields.
// Call only in a networkless decoder container with memory, PID and tmpfs limits.
type Normalizer struct {
	FFmpeg      string
	FFprobe     string
	TGSRenderer string
	TempDir     string
}

func (n Normalizer) Normalize(ctx context.Context, data []byte, format Format) ([]Frame, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > MaxInputBytes {
		return nil, ErrLimit
	}
	if len(data) == 0 {
		return nil, ErrInvalid
	}
	if format == TGS {
		return n.renderTGS(ctx, data)
	}
	if !validMagic(data, format) {
		return nil, ErrInvalid
	}
	dir, err := os.MkdirTemp(n.TempDir, "sticker-")
	if err != nil {
		return nil, fmt.Errorf("create sticker directory: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "asset")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return nil, fmt.Errorf("write sticker: %w", err)
	}
	return n.renderMedia(ctx, path, format)
}

func validMagic(data []byte, format Format) bool {
	switch format {
	case WebP:
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	case WebM:
		return bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3})
	case TGS:
		return false
	default:
		return false
	}
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		return 0, ErrLimit
	}
	return b.buffer.Write(data)
}

func run(ctx context.Context, executable string, input []byte, limit int, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/nonexistent"}
	command.Stdin = bytes.NewReader(input)
	output := &boundedBuffer{limit: limit}
	command.Stdout = output
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("sticker decoder: %w", err)
	}
	return output.buffer.Bytes(), nil
}

func pngFrame(data []byte, timestamp time.Duration) (Frame, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxSide || config.Height > maxSide {
		return Frame{}, ErrInvalid
	}
	return Frame{PNG: data, Timestamp: timestamp, Width: config.Width, Height: config.Height}, nil
}

func encodeRGBA(data []byte, width, height int, timestamp time.Duration) (Frame, error) {
	if len(data) != width*height*bytesPerPixel {
		return Frame{}, ErrInvalid
	}
	img := &image.RGBA{Pix: data, Stride: width * bytesPerPixel, Rect: image.Rect(0, 0, width, height)}
	var output bytes.Buffer

	if err := png.Encode(&output, img); err != nil {
		return Frame{}, fmt.Errorf("encode sticker frame: %w", err)
	}
	return pngFrame(output.Bytes(), timestamp)
}
