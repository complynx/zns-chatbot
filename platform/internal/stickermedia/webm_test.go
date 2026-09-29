package stickermedia_test

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
)

func webMFixture(t *testing.T, filter string) []byte {
	t.Helper()
	if os.Getenv("STICKER_NATIVE_TEST") != "1" {
		t.Skip("requires contained native decoder image")
	}
	data, err := exec.CommandContext(
		t.Context(),
		"ffmpeg",
		"-v",
		"error",
		"-f",
		"lavfi",
		"-i",
		filter,
		"-frames:v",
		"1",
		"-threads",
		"1",
		"-c:v",
		"libvpx-vp9",
		"-pix_fmt",
		"yuva420p",
		"-f",
		"webm",
		"pipe:1",
	).Output()
	require.NoError(t, err)
	return data
}

func TestWebMAlpha(t *testing.T) {
	t.Parallel()
	fixture := webMFixture(t, "color=red@0.25:size=64x64:rate=1,format=yuva420p")
	normalizer := stickermedia.Normalizer{FFmpeg: "ffmpeg", FFprobe: "ffprobe", TempDir: t.TempDir()}
	frames, err := normalizer.Normalize(t.Context(), fixture, stickermedia.WebM)
	require.NoError(t, err)
	require.Len(t, frames, 1)
	decoded, err := png.Decode(bytes.NewReader(frames[0].PNG))
	require.NoError(t, err)
	_, _, _, alpha := decoded.At(32, 32).RGBA()
	require.InDelta(t, 0.25, float64(alpha)/65535, 0.02, "WebM auxiliary alpha must survive normalization")
}

func TestWebMLongFinalFrame(t *testing.T) {
	t.Parallel()
	fixture := webMFixture(t, "color=red:size=64x64:rate=1/4,format=yuva420p")
	normalizer := stickermedia.Normalizer{FFmpeg: "ffmpeg", FFprobe: "ffprobe", TempDir: t.TempDir()}
	_, err := normalizer.Normalize(t.Context(), fixture, stickermedia.WebM)
	require.ErrorIs(t, err, stickermedia.ErrInvalid, "frame starts at zero but ends after four seconds")
}
