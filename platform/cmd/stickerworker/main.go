package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/stickerclient"
	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
	"github.com/complynx/zns-chatbot/platform/internal/stickerworker"
)

const socketPath = "/run/sticker-ipc/decoder.sock"

func main() {
	if err := serve(); err != nil {
		log.Fatal(err)
	}
}

func serve() error {
	if os.Getenv("STICKER_WORKER_MODE") == "decoder" {
		return serveDecoder()
	}
	if mode := os.Getenv("STICKER_WORKER_MODE"); mode != "" && mode != "broker" {
		return errors.New("unknown sticker worker mode")
	}
	client, err := stickerclient.NewUnix(socketPath)
	if err != nil {
		return err
	}
	handler, err := stickerworker.New(client, os.Getenv("STICKER_WORKER_SECRET"))
	if err != nil {
		return err
	}
	return serverFor(handler).ListenAndServe()
}

func serveDecoder() error {
	if os.Getenv("STICKER_WORKER_SECRET") != "" || os.Getenv("OPENAI_API_KEY") != "" {
		return errors.New("decoder must not receive secrets")
	}
	// Native children receive their own minimal environment; also clear the worker's environment.
	os.Clearenv()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return err
	}
	directory, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		return err
	}
	const privateMode = 0700
	if directory.Mode().Perm() != privateMode {
		return errors.New("sticker IPC directory must be private")
	}
	if info, statErr := os.Lstat(socketPath); statErr == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("sticker socket path is not a socket")
		}
		if err = os.Remove(socketPath); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	var config net.ListenConfig
	listener, err := config.Listen(context.Background(), "unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socketPath, 0600); err != nil {
		return err
	}
	handler, err := stickerworker.New(
		stickermedia.Normalizer{
			FFmpeg:      "/usr/bin/ffmpeg",
			FFprobe:     "/usr/bin/ffprobe",
			TGSRenderer: "/usr/local/bin/tgs-render",
			TempDir:     "/tmp",
		},
		stickerclient.DecoderAuthorization,
	)
	if err != nil {
		return err
	}
	return serverFor(handler).Serve(listener)
}

func serverFor(handler http.Handler) *http.Server {
	const headerTimeout = 5 * time.Second
	const writeTimeout = 25 * time.Second
	const maxHeaders = 8192
	return &http.Server{
		Addr:              ":8098",
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
		ReadTimeout:       stickerclient.Timeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       headerTimeout,
		MaxHeaderBytes:    maxHeaders,
	}
}
