package renderer_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
)

func quadrantPhoto(t *testing.T) []byte {
	t.Helper()
	photo := image.NewNRGBA(image.Rect(0, 0, 80, 80))
	for y := range 80 {
		for x := range 80 {
			pixel := color.NRGBA{R: 240, A: 255}
			if x >= 40 {
				pixel = color.NRGBA{G: 240, A: 255}
			}
			if y >= 40 {
				pixel = color.NRGBA{B: 240, A: 255}
				if x >= 40 {
					pixel = color.NRGBA{R: 240, G: 240, A: 255}
				}
			}
			photo.SetNRGBA(x, y, pixel)
		}
	}
	return pngBytes(t, photo)
}

func TestEditedRenderPanRotationAndZoomFollowFrameCoordinates(t *testing.T) {
	t.Parallel()
	raw := quadrantPhoto(t)
	for _, transform := range []avatar.Transform{
		{ZoomPPM: avatar.FramePPM, OffsetXPPM: avatar.FramePPM / 4},
		{ZoomPPM: avatar.FramePPM, AngleMilli: 90000},
	} {
		rendered, err := renderer.Render(t.Context(), raw, transform)
		require.NoError(t, err)
		require.True(t, rendered.MatchesTransform(transform))
		photo, err := jpeg.Decode(bytes.NewReader(rendered.JPEG))
		require.NoError(t, err)
		r, g, b, _ := photo.At(600, 400).RGBA()
		require.Greater(t, r, g)
		require.Greater(t, r, b)
		again, err := renderer.Render(t.Context(), raw, transform)
		require.NoError(t, err)
		require.Equal(t, rendered.JPEG, again.JPEG)
	}
	photo := image.NewNRGBA(image.Rect(0, 0, 120, 40))
	for y := range 40 {
		for x := range 120 {
			pixel := color.NRGBA{R: 240, A: 255}
			if x >= 40 && x < 80 {
				pixel = color.NRGBA{G: 240, A: 255}
			}
			photo.SetNRGBA(x, y, pixel)
		}
	}
	rendered, err := renderer.Render(t.Context(), pngBytes(t, photo), avatar.Transform{ZoomPPM: avatar.MinZoomPPM})
	require.NoError(t, err)
	output, err := jpeg.Decode(bytes.NewReader(rendered.JPEG))
	require.NoError(t, err)
	r, g, _, _ := output.At(800, 512).RGBA()
	require.Greater(t, r, g, "Zooming out reveals original side content")
}

func TestEditedRenderUsesExifOrientationBeforeTranslation(t *testing.T) {
	t.Parallel()
	transform := avatar.Transform{ZoomPPM: avatar.FramePPM, OffsetYPPM: avatar.FramePPM / 4}
	rendered, err := renderer.Render(t.Context(), orientedJPEG(t), transform)
	require.NoError(t, err)
	require.NotContains(t, string(rendered.JPEG), "Exif")
	output, err := jpeg.Decode(bytes.NewReader(rendered.JPEG))
	require.NoError(t, err)
	r, _, b, _ := output.At(512, 512).RGBA()
	require.Greater(t, r, b)
}

func TestEditedRenderExtremeAspectAndOffsetsStayOutputBounded(t *testing.T) {
	t.Parallel()
	photo := image.NewNRGBA(image.Rect(0, 0, avatar.MaxDimension, 1))
	transform := avatar.Transform{ZoomPPM: avatar.MaxZoomPPM, OffsetXPPM: avatar.MaxOffsetPPM, AngleMilli: 45000}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	rendered, err := renderer.Render(ctx, pngBytes(t, photo), transform)
	require.NoError(t, err)
	require.LessOrEqual(t, len(rendered.JPEG), avatar.MaxOutputBytes)
	output, err := jpeg.Decode(bytes.NewReader(rendered.JPEG))
	require.NoError(t, err)
	r, g, b, _ := output.At(512, 512).RGBA()
	require.Greater(t, r, uint32(250*257))
	require.Greater(t, g, uint32(250*257))
	require.Greater(t, b, uint32(250*257))
}

func TestEditedRenderZoomOutAveragesDetailAcrossSmallTranslations(t *testing.T) {
	t.Parallel()
	photo := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	for y := range 2048 {
		for x := range 2048 {
			shade := uint8(0)
			if x%8 >= 4 {
				shade = 255
			}
			photo.SetNRGBA(x, y, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	raw := pngBytes(t, photo)
	for _, offset := range []int64{0, avatar.FramePPM / (4 * avatar.OutputSize)} {
		output, err := renderer.Render(
			t.Context(),
			raw,
			avatar.Transform{ZoomPPM: avatar.MinZoomPPM, OffsetXPPM: offset},
		)
		require.NoError(t, err)
		decoded, err := jpeg.Decode(bytes.NewReader(output.JPEG))
		require.NoError(t, err)
		r, g, b, _ := decoded.At(512, 512).RGBA()
		for _, channel := range []uint32{r, g, b} {
			require.InDelta(
				t,
				128*257,
				channel,
				20*257,
				"Shrinking averages stripes instead of selecting a color during a small pan",
			)
		}
	}
}
