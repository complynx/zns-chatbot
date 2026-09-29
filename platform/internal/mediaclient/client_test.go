package mediaclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

func audioResult() mediaproc.Result {
	return mediaproc.Result{
		Status:     "ready",
		Duration:   mediaproc.Rational{Numerator: 1, Denominator: 1},
		Transcript: mediaproc.Transcript{Status: "ok", Text: "Hello"},
	}
}

func videoResult(t *testing.T) mediaproc.Result {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	return mediaproc.Result{
		Status:   "ready",
		Duration: mediaproc.Rational{Numerator: 2, Denominator: 1},
		Transcript: mediaproc.Transcript{
			Status: "not_run",
		},
		Frames: []mediaproc.Frame{
			{JPEG: buffer.Bytes(), Width: 2, Height: 2, Timestamp: mediaproc.Rational{Numerator: 1, Denominator: 1}},
		},
		Sampling: &mediaproc.Sampling{
			Coverage:  "range_sparse",
			Requested: []mediaproc.Rational{{Numerator: 1, Denominator: 1}},
		},
	}
}

func TestAuthenticatedPreprocessAndRange(t *testing.T) {
	t.Parallel()
	video := videoResult(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, []byte("raw media"), body)
		result := audioResult()
		if r.URL.Path == "/v1/storyboard" {
			assert.Equal(t, "video_note", r.URL.Query().Get("kind"))
			assert.Equal(t, "1000", r.URL.Query().Get("start_ms"))
			assert.Equal(t, "2000", r.URL.Query().Get("end_ms"))
			assert.Equal(t, "1", r.URL.Query().Get("count"))
			result = video
		} else {
			assert.Equal(t, "/v1/preprocess", r.URL.Path)
			assert.Equal(t, "voice", r.URL.Query().Get("kind"))
		}
		assert.NoError(t, json.NewEncoder(w).Encode(result))
	}))
	defer server.Close()
	client, err := mediaclient.New(mediaclient.Config{URL: server.URL, Secret: "test-secret"})
	require.NoError(t, err)
	result, err := client.Preprocess(t.Context(), mediaclient.Voice, []byte("raw media"))
	require.NoError(t, err)
	assert.Equal(t, "Hello", result.Transcript.Text)
	result, err = client.Storyboard(
		t.Context(),
		mediaclient.VideoNote,
		[]byte("raw media"),
		mediaclient.Range{StartMS: 1000, EndMS: 2000, Count: 1},
	)
	require.NoError(t, err)
	assert.Equal(t, video, result)
}

func TestRejectsMalformedResponses(t *testing.T) {
	t.Parallel()
	valid, err := json.Marshal(audioResult())
	require.NoError(t, err)
	for index, body := range []string{"{}", "null", string(valid) + " {}", strings.Replace(string(valid), `"ready"`, `"unknown"`, 1), strings.Replace(string(valid), `"denominator":1`, `"denominator":0`, 1), string(valid[:len(valid)-1]) + `,"unexpected":true}`, strings.Repeat("x", (4<<20)+1)} {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, writeErr := io.WriteString(w, body)
				assert.NoError(t, writeErr)
			}))
			defer server.Close()
			client, newErr := mediaclient.New(mediaclient.Config{URL: server.URL, Secret: "secret"})
			require.NoError(t, newErr)
			_, getErr := client.Preprocess(t.Context(), mediaclient.Audio, []byte("audio"))
			require.ErrorIs(t, getErr, mediaclient.ErrResponse)
		})
	}
}

func TestRejectsInvalidArtifacts(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*mediaproc.Result){
		func(r *mediaproc.Result) { r.Frames[0].JPEG = []byte("not JPEG") },
		func(r *mediaproc.Result) { r.Frames[0].Width = 3 },
		func(r *mediaproc.Result) { r.Frames[0].Timestamp.Numerator = 2 },
		func(r *mediaproc.Result) { r.Sampling.Coverage = "uniform_sparse" },
		func(r *mediaproc.Result) { r.Sampling.Requested[0].Numerator = 0 },
	} {
		result := videoResult(t)
		mutate(&result)
		server := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { assert.NoError(t, json.NewEncoder(w).Encode(result)) },
			),
		)
		client, err := mediaclient.New(mediaclient.Config{URL: server.URL, Secret: "secret"})
		require.NoError(t, err)
		_, err = client.Storyboard(
			t.Context(),
			mediaclient.Video,
			[]byte("video"),
			mediaclient.Range{StartMS: 1000, EndMS: 2000, Count: 1},
		)
		server.Close()
		require.ErrorIs(t, err, mediaclient.ErrResponse)
	}
}

func TestCancellationRedirectAndInputBounds(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := mediaclient.New(mediaclient.Config{URL: server.URL, Secret: "secret"})
	require.NoError(t, err)
	_, err = client.Preprocess(t.Context(), mediaclient.Audio, []byte("audio"))
	var status *mediaclient.HTTPError
	require.ErrorAs(t, err, &status)
	assert.Equal(t, http.StatusTemporaryRedirect, status.Status)
	assert.EqualValues(t, 1, calls.Load())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Preprocess(ctx, mediaclient.Audio, []byte("audio"))
	require.ErrorIs(t, err, context.Canceled)
	_, err = client.Preprocess(t.Context(), mediaclient.Audio, make([]byte, mediaproc.MaxInputBytes+1))
	require.ErrorIs(t, err, mediaclient.ErrInput)
	_, err = client.Storyboard(
		t.Context(),
		mediaclient.Audio,
		[]byte("audio"),
		mediaclient.Range{EndMS: 1000, Count: 1},
	)
	require.ErrorIs(t, err, mediaclient.ErrInput)
	assert.EqualValues(t, 1, calls.Load())
	for _, address := range []string{"file:///tmp/a", server.URL + "?url=elsewhere", "http://user:pass@host", server.URL + "/path"} {
		_, err = mediaclient.New(mediaclient.Config{URL: address, Secret: "secret"})
		require.ErrorIs(t, err, mediaclient.ErrConfiguration)
	}
}

func TestCancelInFlight(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := mediaclient.New(mediaclient.Config{URL: server.URL, Secret: "secret"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, callErr := client.Preprocess(ctx, mediaclient.Voice, []byte("audio")); done <- callErr }()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWorkerOutcomes(t *testing.T) {
	t.Parallel()
	video := videoResult(t)
	video.Transcript = mediaproc.Transcript{Status: "no_audio"}
	video.Sampling.Coverage = "uniform_sparse"
	video.Sampling.Requested[0].Numerator = 0
	for _, status := range []string{"ready", "partial", "failed", "rejected"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			result := video
			result.Status = status
			kind := mediaclient.Video
			switch status {
			case "partial":
				result.Reason = "transcription_failed"
				result.Transcript = mediaproc.Transcript{Status: "failed"}
			case "failed":
				result = audioResult()
				result.Status = status
				result.Reason = "transcription_unavailable"
				result.Transcript = mediaproc.Transcript{Status: "failed"}
				kind = mediaclient.Audio
			case "rejected":
				result = mediaproc.Result{
					Status:     status,
					Reason:     "too_long",
					Duration:   mediaproc.Rational{Denominator: 1},
					Transcript: mediaproc.Transcript{Status: "not_run"},
				}
			}
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, _ *http.Request) { assert.NoError(t, json.NewEncoder(w).Encode(result)) },
				),
			)
			defer server.Close()
			client, err := mediaclient.New(mediaclient.Config{URL: server.URL, Secret: "secret"})
			require.NoError(t, err)
			actual, err := client.Preprocess(t.Context(), kind, []byte("media"))
			require.NoError(t, err)
			assert.Equal(t, result, actual)
		})
	}
}
