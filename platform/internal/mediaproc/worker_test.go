package mediaproc_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

func TestWorkerRealFFmpeg(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.NoError(t, r.ParseMultipartForm(10<<20))
		assert.Equal(t, "gpt-transcribe", r.FormValue("model"))
		f, h, e := r.FormFile("file")
		if !assert.NoError(t, e) {
			return
		}
		defer f.Close()
		assert.Equal(t, "audio.wav", h.Filename)
		assert.Equal(t, "audio/wav", h.Header.Get("Content-Type"))
		w.Write([]byte(`{"text":"Synthetic contract transcript"}`))
	}))
	defer provider.Close()
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      "test-secret",
			APIKey:      "test-key",
			APIURL:      provider.URL,
			TempRoot:    t.TempDir(),
			HTTPClient:  provider.Client(),
		},
	)
	require.NoError(t, err)
	for _, fixture := range []struct{ name, source, codec, extension, kind, status, reason string }{
		{"wav_exact", "anullsrc=r=16000:cl=mono:d=240", "pcm_s16le", "wav", "audio", "ready", ""},
		{"wav_over", "anullsrc=r=16000:cl=mono:d=240.001", "pcm_s16le", "wav", "audio", "rejected", "too_long"},
		{"opus_exact", "sine=frequency=440:sample_rate=48000:duration=240", "libopus", "ogg", "voice", "ready", ""},
		{"aac_exact", "sine=frequency=440:sample_rate=48000:duration=240", "aac", "m4a", "audio", "ready", ""},
		{"aac_padding_exact", "sine=frequency=440:sample_rate=44100:duration=240", "aac", "m4a", "audio", "ready", ""},
		{"aac_padding_over", "sine=frequency=440:sample_rate=44100:duration=240.01", "aac", "m4a", "audio", "rejected", "too_long"},
		{"opus_over", "sine=frequency=440:sample_rate=48000:duration=240.001", "libopus", "ogg", "voice", "rejected", "too_long"},
	} {
		t.Log(fixture.name)
		path := filepath.Join(t.TempDir(), "fixture."+fixture.extension)
		out, e := exec.CommandContext(t.Context(), "/usr/bin/ffmpeg", "-v", "error", "-f", "lavfi", "-i", fixture.source, "-c:a", fixture.codec, "-y", path).
			CombinedOutput()
		require.NoError(t, e, string(out))
		data, e := os.ReadFile(path)
		require.NoError(t, e)
		before := calls
		r := httptest.NewRequest(http.MethodPost, "/v1/preprocess?kind="+fixture.kind, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer test-secret")
		w := httptest.NewRecorder()
		worker.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		var result mediaproc.Result
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, fixture.status, result.Status, "%+v", result)
		require.Equal(t, fixture.reason, result.Reason)
		if fixture.reason != "" {
			require.Equal(t, before, calls)
			require.Empty(t, result.Frames)
		}
	}
}

func TestVideoStoryboard(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      "test-secret",
			TempRoot:    t.TempDir(),
		},
	)
	require.NoError(t, err)
	directory := t.TempDir()
	for _, duration := range []string{"240", "240.25"} {
		path := filepath.Join(directory, "video-"+duration+".mp4")
		output, encodeErr := exec.CommandContext(t.Context(), "/usr/bin/ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x120:r=4:d="+duration, "-c:v", "libx264", "-y", path).
			CombinedOutput()
		require.NoError(t, encodeErr, string(output))
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		if duration == "240.25" {
			data = spoofHeaderDuration(t, data)
		}
		for _, endpoint := range []string{"/v1/preprocess?kind=video", "/v1/storyboard?kind=video&start_ms=60000&end_ms=62000&count=4"} {
			request := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
			request.Header.Set("Authorization", "Bearer test-secret")
			recorder := httptest.NewRecorder()
			worker.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code)
			var result mediaproc.Result
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
			if duration == "240.25" {
				require.Equal(t, "too_long", result.Reason)
				require.Empty(t, result.Frames)
				require.Equal(t, "not_run", result.Transcript.Status)
				continue
			}
			require.Equal(t, "ready", result.Status, "%+v", result)
			require.NotEmpty(t, result.Frames)
			require.LessOrEqual(t, len(result.Frames), 8)
			for _, frame := range result.Frames {
				require.LessOrEqual(t, len(frame.JPEG), 256<<10)
				require.LessOrEqual(t, frame.Width, 768)
				require.LessOrEqual(t, frame.Height, 768)
			}
			if result.Sampling.Coverage == "range_sparse" {
				require.Len(t, result.Frames, 4)
				require.GreaterOrEqual(
					t,
					result.Frames[0].Timestamp.Numerator/result.Frames[0].Timestamp.Denominator,
					int64(60),
				)
				require.Equal(t, "not_run", result.Transcript.Status)
			} else {
				require.Len(t, result.Frames, 8)
				require.Equal(t, "no_audio", result.Transcript.Status)
			}
		}
	}
}
func spoofHeaderDuration(t *testing.T, data []byte) []byte {
	t.Helper()
	for _, atom := range []string{"mvhd", "mdhd"} {
		offset := bytes.Index(data, []byte(atom))
		require.Positive(t, offset)
		// Version-zero timing fields follow flags and two creation timestamps.
		require.Zero(t, data[offset+4])
		scale := binary.BigEndian.Uint32(data[offset+16 : offset+20])
		binary.BigEndian.PutUint32(data[offset+20:offset+24], scale)
	}
	return data
}

func TestWorkerAuthentication(t *testing.T) {
	t.Parallel()
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFmpegPath:  filepath.Join(t.TempDir(), "ffmpeg"),
			FFprobePath: filepath.Join(t.TempDir(), "ffprobe"),
			Secret:      "secret",
		},
	)
	require.NoError(t, err)
	for _, token := range []string{"", "Bearer wrong"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/preprocess?kind=audio", nil)
		request.Header.Set("Authorization", token)
		recorder := httptest.NewRecorder()
		worker.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
}

func TestDurationVersionsAndEarlyRejection(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("requires POSIX shell for trusted fake probe")
	}
	for _, field := range []string{"duration", "pkt_duration"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			probe := filepath.Join(t.TempDir(), "probe")
			script := `#!/bin/sh
for argument in "$@"; do
 if [ "$argument" = "-show_streams" ]; then
  printf '%s' '{"streams":[{"index":0,"codec_name":"h264","codec_type":"video","time_base":"1/1","width":16,"height":16}]}'
  exit 0
 fi
done
printf '%s' '{"frames":[{"stream_index":0,"pts":0,"` + field + `":1,"width":16,"height":16},{"stream_index":0,"pts":240,"` + field + `":1,"width":16,"height":16}'
while :; do printf '%s' ',{"stream_index":0,"pts":241,"` + field + `":1,"width":16,"height":16}'; done
`
			require.NoError(t, os.WriteFile(probe, []byte(script), 0700))
			worker, err := mediaproc.New(
				mediaproc.Config{
					FFprobePath: probe,
					FFmpegPath:  filepath.Join(t.TempDir(), "must-not-execute"),
					Secret:      "secret",
				},
			)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/v1/preprocess?kind=video", strings.NewReader("test"))
			request.Header.Set("Authorization", "Bearer secret")
			recorder := httptest.NewRecorder()
			started := time.Now()
			worker.ServeHTTP(recorder, request)
			require.Less(t, time.Since(started), 5*time.Second)
			var result mediaproc.Result
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
			require.Equal(t, "too_long", result.Reason)
			require.Empty(t, result.Frames)
			require.Equal(t, "not_run", result.Transcript.Status)
		})
	}
}

func TestFrameMetadataBudgetStopsProducer(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("requires POSIX shell for trusted fake probe")
	}
	probe := filepath.Join(t.TempDir(), "probe")
	script := `#!/bin/sh
for argument in "$@"; do
 if [ "$argument" = "-show_streams" ]; then
  printf '%s' '{"streams":[{"index":0,"codec_name":"h264","codec_type":"video","time_base":"1/1","width":16,"height":16}]}'
  exit 0
 fi
done
printf '%s' '{"frames":['
while :; do printf '%s' '` + strings.Repeat(" ", 4096) + `'; done
`
	require.NoError(t, os.WriteFile(probe, []byte(script), 0700))
	worker, err := mediaproc.New(
		mediaproc.Config{
			FFprobePath: probe,
			FFmpegPath:  filepath.Join(t.TempDir(), "must-not-execute"),
			Secret:      "secret",
		},
	)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/preprocess?kind=video", strings.NewReader("test"))
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	started := time.Now()
	worker.ServeHTTP(recorder, request)
	require.Less(t, time.Since(started), 5*time.Second)
	var result mediaproc.Result
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.Equal(t, "unreadable_duration", result.Reason)
	require.Empty(t, result.Frames)
}

func requireMediaTools(t *testing.T) {
	t.Helper()
	for _, path := range []string{"/usr/bin/ffmpeg", "/usr/bin/ffprobe"} {
		if _, err := os.Stat(path); err != nil {
			if os.Getenv("MEDIA_TEST_REQUIRE_FFMPEG") == "1" {
				t.Fatalf("required media test tool %s unavailable: %v", path, err)
			}
			t.Skip("real media fixtures require Linux ffmpeg and ffprobe")
		}
	}
}
