package stickerworker_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/stickerworker"

	"github.com/complynx/zns-chatbot/platform/internal/stickerclient"
	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
)

type decoderFunc func(context.Context, []byte, stickermedia.Format) ([]stickermedia.Frame, error)

func (f decoderFunc) Normalize(
	ctx context.Context,
	data []byte,
	format stickermedia.Format,
) ([]stickermedia.Frame, error) {
	return f(ctx, data, format)
}

func TestAdmissionAndAuthorization(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	var output bytes.Buffer
	require.NoError(t, png.Encode(&output, image.NewNRGBA(image.Rect(0, 0, 1, 1))))
	worker, err := stickerworker.New(
		decoderFunc(func(ctx context.Context, _ []byte, _ stickermedia.Format) ([]stickermedia.Frame, error) {
			close(entered)
			select {
			case <-release:
				return []stickermedia.Frame{{PNG: output.Bytes(), Width: 1, Height: 1}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}),
		"secret",
	)
	require.NoError(t, err)
	call := func(secret string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost,
			stickerclient.Path+"?format=webp",
			bytes.NewReader([]byte("input")),
		)
		request.Header.Set("Authorization", "Bearer "+secret)
		result := httptest.NewRecorder()
		worker.ServeHTTP(result, request)
		return result
	}
	require.Equal(t, http.StatusUnauthorized, call("bad").Code)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- call("secret") }()
	<-entered
	require.Equal(t, http.StatusServiceUnavailable, call("secret").Code)
	close(release)
	require.Equal(t, http.StatusOK, (<-done).Code)
}

func TestInputLimits(t *testing.T) {
	t.Parallel()

	worker, err := stickerworker.New(
		decoderFunc(func(context.Context, []byte, stickermedia.Format) ([]stickermedia.Frame, error) {
			t.Error("decoder invoked")
			return nil, stickermedia.ErrInvalid
		}),
		"secret",
	)
	require.NoError(t, err)
	for _, size := range []int{0, stickermedia.MaxInputBytes + 1} {
		request := httptest.NewRequest(
			http.MethodPost,
			stickerclient.Path+"?format=tgs",
			bytes.NewReader(make([]byte, size)),
		)
		request.Header.Set("Authorization", "Bearer secret")
		result := httptest.NewRecorder()
		worker.ServeHTTP(result, request)
		require.Equal(t, http.StatusRequestEntityTooLarge, result.Code)
	}
}
