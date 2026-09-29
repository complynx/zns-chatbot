package mediaproc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

func TestBrokerUnixDecoderAndFakeASR(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	directory := t.TempDir()
	socket := filepath.Join(directory, "decoder.sock")
	decoder, err := mediaproc.New(
		mediaproc.Config{
			DecodeOnly:  true,
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      mediaproc.DecoderAuthorization,
			TempRoot:    directory,
		},
	)
	require.NoError(t, err)
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: decoder}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	t.Cleanup(func() {
		require.NoError(t, server.Shutdown(context.Background()))
		require.ErrorIs(t, <-finished, http.ErrServerClosed)
	})
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		assert.Equal(t, "Bearer synthetic-provider-key", request.Header.Get("Authorization"))
		assert.NoError(t, request.ParseMultipartForm(10<<20))
		assert.Equal(t, "gpt-transcribe", request.FormValue("model"))
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(map[string]string{"text": "Synthetic spoken content"})
	}))
	t.Cleanup(provider.Close)
	broker, err := mediaproc.NewBroker(
		mediaproc.BrokerConfig{
			Secret:     "secret",
			APIKey:     "synthetic-provider-key",
			APIURL:     provider.URL,
			SocketPath: socket,
			HTTPClient: provider.Client(),
		},
	)
	require.NoError(t, err)
	for _, duration := range []string{"1", "240.001"} {
		audio := filepath.Join(directory, "audio.wav")
		encodeFixture(
			t,
			"-f",
			"lavfi",
			"-i",
			"sine=frequency=440:sample_rate=16000:duration="+duration,
			"-c:a",
			"pcm_s16le",
			audio,
		)
		result := preprocessHandlerFixture(t, broker, audio, "/v1/preprocess?kind=audio&url=http://invalid.example")
		if duration == "1" {
			require.Equal(t, "ready", result.Status)
			require.Equal(t, "Synthetic spoken content", result.Transcript.Text)
		} else {
			require.Equal(t, "too_long", result.Reason)
			require.Equal(t, "not_run", result.Transcript.Status)
		}
	}
	require.EqualValues(t, 1, calls.Load(), "long input cannot reach ASR")
	aac := filepath.Join(directory, "padding.m4a")
	encodeFixture(t, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=240", "-c:a", "aac", aac)
	padding := preprocessHandlerFixture(t, broker, aac, "/v1/preprocess?kind=audio")
	require.Equal(t, "ready", padding.Status)
	require.Equal(t, mediaproc.Rational{Numerator: 240, Denominator: 1}, padding.Duration)
	video := filepath.Join(directory, "video.mp4")
	encodeFixture(t, "-f", "lavfi", "-i", "color=c=blue:s=160x120:r=4:d=2", "-c:v", "libx264", video)
	result := preprocessHandlerFixture(t, broker, video, "/v1/storyboard?kind=video&start_ms=0&end_ms=1000&count=2")
	require.Equal(t, "ready", result.Status)
	require.Len(t, result.Frames, 2)
	require.EqualValues(t, 2, calls.Load(), "range extraction never calls ASR")
	_, err = mediaproc.New(
		mediaproc.Config{
			DecodeOnly:  true,
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      "secret",
			APIKey:      "must-not-enter-decoder",
		},
	)
	require.Error(t, err)
}
func preprocessHandlerFixture(t *testing.T, handler http.Handler, path, endpoint string) mediaproc.Result {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	request := httptest.NewRequest(http.MethodPost, endpoint, file)
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var result mediaproc.Result
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	return result
}

func TestBrokerRejectsMalformedDecoderAndCancels(t *testing.T) {
	t.Parallel()
	requireMediaTools(t)
	socket := filepath.Join(t.TempDir(), "decoder.sock")
	started := make(chan struct{})
	cancelled := make(chan struct{})
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		io.Copy(io.Discard, request.Body)
		if request.URL.Query().Get("kind") == "voice" {
			close(started)
			<-request.Context().Done()
			close(cancelled)
			return
		}
		if request.URL.Query().Get("kind") == "video" {
			writer.Write(bytes.Repeat([]byte(" "), (16<<20)+1))
			return
		}
		writer.Write(
			[]byte(
				`{"result":{"status":"ready","duration":{"numerator":241,"denominator":1},"transcript":{"status":"not_run"}},"wav":"UklGRg=="}`,
			),
		)
	})}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	t.Cleanup(func() {
		require.NoError(t, server.Shutdown(context.Background()))
		require.ErrorIs(t, <-finished, http.ErrServerClosed)
	})
	broker, err := mediaproc.NewBroker(mediaproc.BrokerConfig{Secret: "secret", SocketPath: socket})
	require.NoError(t, err)
	for _, kind := range []string{"audio", "video"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/preprocess?kind="+kind, strings.NewReader("synthetic"))
		request.Header.Set("Authorization", "Bearer secret")
		recorder := httptest.NewRecorder()
		broker.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadGateway, recorder.Code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/v1/preprocess?kind=voice",
		strings.NewReader("synthetic"),
	)
	request.Header.Set("Authorization", "Bearer secret")
	done := make(chan struct{})
	go func() { broker.ServeHTTP(httptest.NewRecorder(), request); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("decoder did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("decoder request was not cancelled")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broker did not finish")
	}
}
