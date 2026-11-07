package renderer_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	avatarframe "github.com/complynx/zns-chatbot/internal/avatar/frame"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
)

func pngBytes(t *testing.T, src image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, src))
	return b.Bytes()
}

func TestRenderCentersPhotoAndAppliesTrustedFrame(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 120, 40))
	for y := range 40 {
		for x := range 120 {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 40 && x < 80 {
				c = color.NRGBA{G: 220, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	raw := pngBytes(t, src)
	result, err := renderer.Render(context.Background(), raw, avatar.IdentityTransform())
	require.NoError(t, err)
	output, err := jpeg.Decode(bytes.NewReader(result.JPEG))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 1024, 1024), output.Bounds())
	frame, err := io.ReadAll(avatarframe.Reader())
	require.NoError(t, err)
	frameHash := sha256.Sum256(frame)
	require.Equal(t, avatar.FrameSHA256, hex.EncodeToString(frameHash[:]))
	require.Equal(t, avatar.FrameSHA256, result.FrameSHA256)
	require.Equal(t, avatar.ProcessorVersion, result.ProcessorVersion)
	require.Nil(t, result.Transform)
	require.Equal(
		t,
		"31a754361b05145895831fdd0e57e801a8442c7caff8127d116f3c59ea077728",
		result.SHA256,
		"Legacy centered JPEG bytes must remain unchanged",
	)
	// The middle of the square comes from the central green strip, never the red ends.
	centerR, centerG, centerB, _ := output.At(512, 512).RGBA()
	require.Greater(t, centerG, centerR)
	require.Greater(t, centerG, centerB)
	// The actual frame contributes pixels: framing is not just a square resize.
	plain := color.NRGBA{G: 220, A: 255}
	different := false
	for y := 0; y < 1024; y += 32 {
		for x := 0; x < 1024; x += 32 {
			r, g, b, _ := output.At(x, y).RGBA()
			if r > 20*257 || b > 20*257 || g < uint32(plain.G-20)*257 {
				different = true
			}
		}
	}
	require.True(t, different)
}

func orientedJPEG(t *testing.T) []byte {
	t.Helper()
	src := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	for y := range 40 {
		for x := range 80 {
			c := color.NRGBA{R: 240, A: 255}
			if x >= 40 {
				c = color.NRGBA{B: 240, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	var raw bytes.Buffer
	require.NoError(t, jpeg.Encode(&raw, src, &jpeg.Options{Quality: 100}))
	// Minimal little-endian EXIF IFD: orientation 6 means rotate 90 degrees clockwise.
	exif := []byte{
		'E',
		'x',
		'i',
		'f',
		0,
		0,
		'I',
		'I',
		42,
		0,
		8,
		0,
		0,
		0,
		1,
		0,
		0x12,
		1,
		3,
		0,
		1,
		0,
		0,
		0,
		6,
		0,
		0,
		0,
		0,
		0,
		0,
		0,
	}
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(exif)+2))
	out := append([]byte{}, raw.Bytes()[:2]...)
	out = append(out, segment...)
	out = append(out, exif...)
	return append(out, raw.Bytes()[2:]...)
}

func TestRenderHonorsOrientationAndErasesMetadata(t *testing.T) {
	t.Parallel()
	raw := orientedJPEG(t)
	result, err := renderer.Render(context.Background(), raw, avatar.IdentityTransform())
	require.NoError(t, err)
	require.NotContains(t, string(result.JPEG), "Exif")
	output, err := jpeg.Decode(bytes.NewReader(result.JPEG))
	require.NoError(t, err)
	// Clockwise rotation turns the original left/right split into a top/bottom split.
	rTop, _, bTop, _ := output.At(512, 350).RGBA()
	rBottom, _, bBottom, _ := output.At(512, 580).RGBA()
	require.Greater(t, rTop, bTop)
	require.Greater(t, bBottom, rBottom)
}

func TestRenderRejectsMalformedUnsupportedAndOversizeImages(t *testing.T) {
	t.Parallel()
	_, err := renderer.Render(context.Background(), []byte("not an image"), avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrInvalidImage)
	_, err = renderer.Render(context.Background(), []byte("GIF89a"), avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrInvalidImage)
	_, err = renderer.Render(context.Background(), make([]byte, avatar.MaxInputBytes+1), avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrImageLimit)
	// Only a header is needed to reject oversized declared dimensions before full decode.
	raw := pngBytes(t, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	binary.BigEndian.PutUint32(raw[16:20], 8193)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	_, err = renderer.Render(context.Background(), raw, avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrImageLimit)
	binary.BigEndian.PutUint32(raw[16:20], 4000)
	binary.BigEndian.PutUint32(raw[20:24], 2001)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	_, err = renderer.Render(context.Background(), raw, avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrImageLimit)
}
