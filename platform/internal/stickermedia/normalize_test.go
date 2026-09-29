package stickermedia_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"

	"github.com/stretchr/testify/require"
)

const vectorJSON = `{"v":"5.7.4","fr":30,"ip":0,"op":60,"w":64,"h":64,"assets":[],"layers":[{"ddd":0,"ind":1,"ty":1,"nm":"red","sr":1,"sc":"#ff0000","sw":64,"sh":64,"ip":0,"op":60,"st":0,"ks":{"o":{"a":0,"k":100},"r":{"a":0,"k":0},"p":{"a":1,"k":[{"t":0,"s":[0,0,0],"e":[64,0,0],"o":{"x":0.33,"y":0.33},"i":{"x":0.67,"y":0.67}},{"t":60,"s":[64,0,0]}]},"a":{"a":0,"k":[0,0,0]},"s":{"a":0,"k":[100,100,100]}}}]}`

func zipped(t *testing.T, data string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	_, err := writer.Write([]byte(data))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return output.Bytes()
}

func TestRejectUnsafeAssets(t *testing.T) {
	t.Parallel()
	normalizer := stickermedia.Normalizer{TempDir: t.TempDir()}
	_, err := normalizer.Normalize(t.Context(), make([]byte, stickermedia.MaxInputBytes+1), stickermedia.WebP)
	require.ErrorIs(t, err, stickermedia.ErrLimit)
	for _, body := range []string{
		"invalid", strings.Replace(vectorJSON, `"assets":[]`, `"assets":[{"p":"../../etc/passwd","u":""}]`, 1),
		strings.Replace(vectorJSON, `"assets":[]`, `"assets":[{"p":"https://example.test/a.png"}]`, 1),
		strings.Replace(vectorJSON, `"fr":30`, `"fr":0`, 1),
	} {
		_, err = normalizer.Normalize(t.Context(), zipped(t, body), stickermedia.TGS)
		require.ErrorIs(t, err, stickermedia.ErrInvalid)
	}
	_, err = normalizer.Normalize(t.Context(), zipped(t, strings.Repeat(" ", (2<<20)+1)), stickermedia.TGS)
	require.ErrorIs(t, err, stickermedia.ErrLimit)
	for _, format := range []stickermedia.Format{stickermedia.WebP, stickermedia.WebM, stickermedia.TGS, "https://example.test"} {
		_, err = normalizer.Normalize(t.Context(), []byte("wrong format"), format)
		require.ErrorIs(t, err, stickermedia.ErrInvalid)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = normalizer.Normalize(ctx, zipped(t, vectorJSON), stickermedia.TGS)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNativeAssets(t *testing.T) {
	t.Parallel()
	if os.Getenv("STICKER_NATIVE_TEST") != "1" {
		t.Skip("run Dockerfile.sticker for native renderer verification")
	}
	normalizer := stickermedia.Normalizer{
		FFmpeg:      "ffmpeg",
		FFprobe:     "ffprobe",
		TGSRenderer: "tgs-render",
		TempDir:     t.TempDir(),
	}
	tgs, err := normalizer.Normalize(t.Context(), zipped(t, vectorJSON), stickermedia.TGS)
	require.NoError(t, err)
	require.Len(t, tgs, 4)
	require.Equal(t, 1500*time.Millisecond, tgs[3].Timestamp)
	first, err := png.Decode(bytes.NewReader(tgs[0].PNG))
	require.NoError(t, err)
	last, err := png.Decode(bytes.NewReader(tgs[3].PNG))
	require.NoError(t, err)
	require.NotEqual(t, first.At(32, 32), last.At(32, 32), "animation must render different timeline states")
	red, _, _, alpha := first.At(32, 32).RGBA()
	require.Greater(t, red, uint32(60000))
	require.Greater(t, alpha, uint32(60000))
	for _, format := range []stickermedia.Format{stickermedia.WebP, stickermedia.WebM} {
		args := []string{"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=64x64:rate=10", "-t", "2", "-threads", "1"}
		if format == stickermedia.WebP {
			args = append(args, "-frames:v", "1", "-c:v", "libwebp", "-f", "webp", "pipe:1")
		} else {
			args = append(args, "-c:v", "libvpx-vp9", "-f", "webm", "pipe:1")
		}
		fixture, fixtureErr := exec.CommandContext(t.Context(), "ffmpeg", args...).Output()
		require.NoError(t, fixtureErr)
		frames, decodeErr := normalizer.Normalize(t.Context(), fixture, format)
		require.NoError(t, decodeErr)
		expected := 4
		if format == stickermedia.WebP {
			expected = 1
		}
		require.Len(t, frames, expected)
		require.Equal(t, 64, frames[0].Width)
		if format == stickermedia.WebM {
			require.Equal(t, 1500*time.Millisecond, frames[3].Timestamp)
		}
	}
	entries, err := os.ReadDir(normalizer.TempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
	normalizer.TGSRenderer = "/usr/bin/yes"
	_, err = normalizer.Normalize(t.Context(), zipped(t, vectorJSON), stickermedia.TGS)
	require.Error(t, err, "subprocess output cap")
	normalizer.TGSRenderer = "/usr/local/bin/sticker-slow-test"
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = normalizer.Normalize(ctx, zipped(t, vectorJSON), stickermedia.TGS)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
