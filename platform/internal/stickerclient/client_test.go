package stickerclient_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/stickerclient"

	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
)

func testFrame(t *testing.T) stickermedia.Frame {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	return stickermedia.Frame{PNG: data.Bytes(), Width: 2, Height: 2}
}

func TestUntrustedFrames(t *testing.T) {
	t.Parallel()

	good := testFrame(t)
	cases := map[string][]stickermedia.Frame{
		"empty":     nil,
		"duplicate": {good, good},
		"too many":  {good, good, good, good, good},
	}
	for name, mutate := range map[string]func(*stickermedia.Frame){
		"negative":   func(f *stickermedia.Frame) { f.Timestamp = -1 },
		"late":       func(f *stickermedia.Frame) { f.Timestamp = 3 * time.Second },
		"dimensions": func(f *stickermedia.Frame) { f.Width++ },
		"oversize":   func(f *stickermedia.Frame) { f.Width = 513 },
		"truncated":  func(f *stickermedia.Frame) { f.PNG = f.PNG[:len(f.PNG)-12] },
		"trailing":   func(f *stickermedia.Frame) { f.PNG = append(bytes.Clone(f.PNG), 0) },
		"large":      func(f *stickermedia.Frame) { f.PNG = make([]byte, (2<<20)+1) },
	} {
		frame := good
		mutate(&frame)
		cases[name] = []stickermedia.Frame{frame}
	}
	for name, frames := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.False(t, stickerclient.ValidFrames(frames, stickermedia.TGS))
		})
	}
	require.True(t, stickerclient.ValidFrames([]stickermedia.Frame{good}, stickermedia.WebP))
	good.Timestamp = time.Second
	require.False(t, stickerclient.ValidFrames([]stickermedia.Frame{good}, stickermedia.WebP))
}

func TestClientProtocol(t *testing.T) {
	t.Parallel()

	frame := testFrame(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		assert.Equal(t, "webp", r.URL.Query().Get("format"))
		assert.NoError(t, json.NewEncoder(w).Encode([]stickermedia.Frame{frame}))
	}))
	t.Cleanup(server.Close)
	client, err := stickerclient.New(server.URL, "secret")
	require.NoError(t, err)
	frames, err := client.Normalize(t.Context(), []byte("input"), stickermedia.WebP)
	require.NoError(t, err)
	require.Equal(t, []stickermedia.Frame{frame}, frames)
}

func TestClientRejectsRedirectAndMalformedResponse(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"[]", `[{"PNG":"bad"}]`, "null", "[] {}"} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }),
			)
			t.Cleanup(server.Close)
			client, err := stickerclient.New(server.URL, "secret")
			require.NoError(t, err)
			_, err = client.Normalize(t.Context(), []byte("input"), stickermedia.WebP)
			require.ErrorIs(t, err, stickerclient.ErrResponse)
		})
	}
	target := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("followed redirect") }),
	)
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	client, err := stickerclient.New(redirect.URL, "secret")
	require.NoError(t, err)
	_, err = client.Normalize(t.Context(), []byte("input"), stickermedia.WebP)
	require.ErrorContains(t, err, "307")
}

func TestClientBoundsFrameAllocation(t *testing.T) {
	t.Parallel()

	body := "[" + strings.Repeat("{},", 99_999) + "{}]"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := stickerclient.New(server.URL, "secret")
	require.NoError(t, err)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_, normalizeErr := client.Normalize(b.Context(), []byte("input"), stickermedia.TGS)
			require.ErrorIs(b, normalizeErr, stickerclient.ErrResponse)
		}
	})
	// A small response must not allocate a Frame for every untrusted array entry.
	require.Less(t, result.AllocedBytesPerOp(), int64(8<<20))
	t.Logf("response bytes: %d; allocated bytes/op: %d", len(body), result.AllocedBytesPerOp())
}

func TestClientFrameArrayProtocol(t *testing.T) {
	t.Parallel()

	frame := testFrame(t)
	frames := make([]stickermedia.Frame, 4)
	for index := range frames {
		frames[index] = frame
		frames[index].Timestamp = time.Duration(index) * 500 * time.Millisecond
	}
	encoded, err := json.Marshal(frames)
	require.NoError(t, err)
	body := string(encoded)
	cases := map[string]string{
		"four frames":    body,
		"fifth frame":    strings.TrimSuffix(body, "]") + ",{}]",
		"unknown field":  strings.Replace(body, "[{", `[{"unknown":true,`, 1),
		"trailing value": body + " {}",
		"trailing junk":  body + " !",
		"missing end":    strings.TrimSuffix(body, "]"),
		"wrong end":      strings.TrimSuffix(body, "]") + "}",
		"non array":      "{}",
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			t.Cleanup(server.Close)
			client, clientErr := stickerclient.New(server.URL, "secret")
			require.NoError(t, clientErr)
			actual, normalizeErr := client.Normalize(t.Context(), []byte("input"), stickermedia.TGS)
			if name == "four frames" {
				require.NoError(t, normalizeErr)
				require.Equal(t, frames, actual)
			} else {
				require.ErrorIs(t, normalizeErr, stickerclient.ErrResponse)
			}
		})
	}
}
