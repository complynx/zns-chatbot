// Package stickerworker serves the bounded sticker normalization protocol.
package stickerworker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/stickerclient"
	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
)

type Decoder interface {
	Normalize(context.Context, []byte, stickermedia.Format) ([]stickermedia.Frame, error)
}

type Worker struct {
	decoder Decoder
	secret  string
	slot    chan struct{}
}

func New(decoder Decoder, secret string) (*Worker, error) {
	if decoder == nil || secret == "" {
		return nil, errors.New("sticker worker requires decoder and authorization")
	}
	return &Worker{decoder: decoder, secret: secret, slot: make(chan struct{}, 1)}, nil
}

func (worker *Worker) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != stickerclient.Path {
		http.NotFound(writer, request)
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+worker.secret)) != 1 {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	format := stickermedia.Format(request.URL.Query().Get("format"))
	if !stickerclient.ValidFormat(format) {
		http.Error(writer, "invalid format", http.StatusBadRequest)
		return
	}
	select {
	case worker.slot <- struct{}{}:
		defer func() { <-worker.slot }()
	default:
		http.Error(writer, "busy", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), stickerclient.Timeout)
	defer cancel()
	data, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, stickermedia.MaxInputBytes))
	if err != nil || len(data) == 0 {
		http.Error(writer, "invalid size", http.StatusRequestEntityTooLarge)
		return
	}
	frames, err := worker.decoder.Normalize(ctx, data, format)
	if err != nil {
		http.Error(writer, "normalization failed", http.StatusUnprocessableEntity)
		return
	}
	if !stickerclient.ValidFrames(frames, format) {
		http.Error(writer, "invalid decoder output", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(writer).Encode(frames); err != nil {
		return
	}
}
