package mediaproc_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

func TestMP3EncoderDelayBoundaries(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      "secret",
			TempRoot:    t.TempDir(),
		},
	)
	require.NoError(t, err)
	for _, duration := range []string{"1", "240", "240.001"} {
		path := filepath.Join(t.TempDir(), "audio.mp3")
		encodeFixture(t, "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono:d="+duration, "-c:a", "libmp3lame", path)
		result := preprocessFixture(t, worker, path, "/v1/preprocess?kind=audio")
		if duration == "240.001" {
			require.Equal(t, "too_long", result.Reason)
			require.Equal(t, "not_run", result.Transcript.Status)
			continue
		}
		require.Equal(t, "ready", result.Status, "%+v", result)
		require.Equal(t, "silent", result.Transcript.Status)
		if duration == "240" {
			require.Equal(t, mediaproc.Rational{Numerator: 240, Denominator: 1}, result.Duration)
		}
	}
}

func TestRotatedAndOffsetVideo(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      "secret",
			TempRoot:    t.TempDir(),
		},
	)
	require.NoError(t, err)
	for _, duration := range []string{"240", "240.25"} {
		directory := t.TempDir()
		base := filepath.Join(directory, "base.mp4")
		portrait := filepath.Join(directory, "portrait.mp4")
		encodeFixture(
			t,
			"-f",
			"lavfi",
			"-i",
			"color=c=blue:s=160x120:r=4:d="+duration,
			"-c:v",
			"libx264",
			"-output_ts_offset",
			"5",
			base,
		)
		encodeFixture(t, "-display_rotation", "90", "-copyts", "-i", base, "-c", "copy", portrait)
		requireFixtureOffset(t, portrait)
		result := preprocessFixture(t, worker, portrait, "/v1/preprocess?kind=video")
		if duration == "240.25" {
			require.Equal(t, "too_long", result.Reason)
			require.Empty(t, result.Frames)
			continue
		}
		require.Equal(t, "ready", result.Status, "%+v", result)
		require.Equal(t, mediaproc.Rational{Numerator: 240, Denominator: 1}, result.Duration)
		require.Len(t, result.Frames, 8)
		require.Greater(t, result.Frames[0].Height, result.Frames[0].Width)
		require.Equal(t, mediaproc.Rational{Denominator: 1}, result.Frames[0].Timestamp)
		selected := preprocessFixture(
			t,
			worker,
			portrait,
			"/v1/storyboard?kind=video&start_ms=60000&end_ms=62000&count=4",
		)
		require.Equal(t, "ready", selected.Status)
		require.Len(t, selected.Frames, 4)
		require.Equal(t, mediaproc.Rational{Numerator: 60, Denominator: 1}, selected.Frames[0].Timestamp)
		skewed := filepath.Join(directory, "skewed.mp4")
		encodeFixture(t, "-display_rotation", "45", "-i", base, "-c", "copy", skewed)
		unsupported := preprocessFixture(t, worker, skewed, "/v1/preprocess?kind=video")
		require.Equal(t, "unsupported", unsupported.Reason)
	}
}

func encodeFixture(t *testing.T, args ...string) {
	t.Helper()
	command := append([]string{"-v", "error", "-y"}, args...)
	output, err := exec.CommandContext(t.Context(), "/usr/bin/ffmpeg", command...).CombinedOutput()
	require.NoError(t, err, string(output))
}
func preprocessFixture(t *testing.T, worker *mediaproc.Worker, path, endpoint string) mediaproc.Result {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	worker.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var result mediaproc.Result
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	return result
}

func TestCommonOriginPreservesDelayedAudio(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      "secret",
			TempRoot:    t.TempDir(),
		},
	)
	require.NoError(t, err)
	for _, duration := range []string{"238", "238.001"} {
		path := filepath.Join(t.TempDir(), "delayed.mov")
		encodeFixture(
			t,
			"-f",
			"lavfi",
			"-i",
			"color=c=blue:s=160x120:r=4:d=238",
			"-itsoffset",
			"2",
			"-f",
			"lavfi",
			"-i",
			"anullsrc=r=16000:cl=mono:d="+duration,
			"-map",
			"0:v",
			"-map",
			"1:a",
			"-c:v",
			"libx264",
			"-c:a",
			"pcm_s16le",
			"-output_ts_offset",
			"5",
			path,
		)
		requireFixtureOffset(t, path)
		result := preprocessFixture(t, worker, path, "/v1/preprocess?kind=video")
		if duration == "238.001" {
			require.Equal(t, "too_long", result.Reason)
			require.Empty(t, result.Frames)
			require.Equal(t, "not_run", result.Transcript.Status)
			continue
		}
		require.Equal(t, "ready", result.Status, "%+v", result)
		require.Equal(t, mediaproc.Rational{Numerator: 240, Denominator: 1}, result.Duration)
		require.Equal(t, "silent", result.Transcript.Status)
		require.Len(t, result.Frames, 8)
		require.Equal(t, mediaproc.Rational{Denominator: 1}, result.Frames[0].Timestamp)
	}
}

func requireFixtureOffset(t *testing.T, path string) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "/usr/bin/ffprobe", "-v", "error", "-show_entries", "format=start_time", "-of", "default=noprint_wrappers=1:nokey=1", path).
		CombinedOutput()
	require.NoError(t, err, string(output))
	require.Equal(t, "5.000000", strings.TrimSpace(string(output)), "fixture must retain its nonzero muxer origin")
}

func TestDecoderDoesNotInheritSecretsOrProxies(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	if os.Getenv("MEDIA_TEST_ENV_CHILD") != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		child := exec.CommandContext(t.Context(), executable, "-test.run=^TestDecoderDoesNotInheritSecretsOrProxies$")
		child.Env = append(os.Environ(), "MEDIA_TEST_ENV_CHILD=1")
		for _, key := range []string{"OPENAI_API_KEY", "MEDIA_WORKER_SECRET", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy", "MEDIA_TEST_SYNTHETIC_SECRET"} {
			child.Env = append(child.Env, key+"=synthetic-test-value")
		}
		output, err := child.CombinedOutput()
		require.NoError(t, err, string(output))
		return
	}
	directory := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"ffprobe", "ffmpeg"} {
		path := filepath.Join(directory, name)
		script := `#!/bin/sh
if [ -n "$OPENAI_API_KEY$MEDIA_WORKER_SECRET$HTTP_PROXY$HTTPS_PROXY$ALL_PROXY$NO_PROXY$http_proxy$https_proxy$all_proxy$no_proxy$MEDIA_TEST_SYNTHETIC_SECRET" ]; then
 printf '%s' 'unexpected inherited environment' >&2
 exit 77
fi
if [ "$LANG" != C ] || [ "$LC_ALL" != C ]; then exit 78; fi
exec /usr/bin/` + name + ` "$@"
`
		require.NoError(t, os.WriteFile(path, []byte(script), 0700))
		paths[name] = path
	}
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  paths["ffmpeg"],
			FFprobePath: paths["ffprobe"],
			Secret:      "secret",
			TempRoot:    t.TempDir(),
		},
	)
	require.NoError(t, err)
	video := filepath.Join(directory, "fixture.mov")
	encodeFixture(
		t,
		"-f",
		"lavfi",
		"-i",
		"color=c=blue:s=160x120:r=4:d=1",
		"-f",
		"lavfi",
		"-i",
		"anullsrc=r=16000:cl=mono:d=1",
		"-c:v",
		"libx264",
		"-c:a",
		"pcm_s16le",
		video,
	)
	result := preprocessFixture(t, worker, video, "/v1/preprocess?kind=video")
	require.Equal(
		t,
		"ready",
		result.Status,
		"metadata probe, streaming scan and both extraction paths must receive clean environments",
	)
	require.Equal(t, "silent", result.Transcript.Status)
	require.Len(t, result.Frames, 1)
}
