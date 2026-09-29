package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

const socketPath = "/run/media-ipc/decoder.sock"

func main() {
	if err := serve(); err != nil {
		log.Fatal(err)
	}
}
func serve() error {
	mode := os.Getenv("MEDIA_WORKER_MODE")
	if mode == "decoder" {
		return serveDecoder()
	}
	if mode != "" && mode != "broker" {
		return errors.New("unknown media worker mode")
	}
	enforce := false
	if raw := os.Getenv("ZNS_CREDITS__ENFORCE"); raw != "" {
		var err error
		enforce, err = strconv.ParseBool(raw)
		if err != nil {
			return errors.New("invalid media accounting mode")
		}
	}
	var recorder credits.Recorder
	if os.Getenv("OPENAI_API_KEY") != "" {
		if os.Getenv("DATABASE_URL") == "" {
			return errors.New("paid media broker requires accounting database")
		}
		name, err := mediaApplicationName()
		if err != nil {
			return err
		}
		db, err := store.OpenNamed(context.Background(), os.Getenv("DATABASE_URL"), name)
		if err != nil {
			return err
		}
		defer db.Close()

		recorder = credits.Service{DB: db, Enforce: enforce}
	}
	broker, err := mediaproc.NewBroker(
		mediaproc.BrokerConfig{
			Accounting:     recorder,
			CreditsEnforce: enforce,
			SocketPath:     socketPath,
			Secret:         os.Getenv("MEDIA_WORKER_SECRET"),
			APIKey:         os.Getenv("OPENAI_API_KEY"),
			Model:          os.Getenv("OPENAI_TRANSCRIPTION_MODEL"),
			APIURL:         os.Getenv("OPENAI_TRANSCRIPTION_URL"),
		},
	)
	if err != nil {
		return err
	}
	server := serverFor(broker)
	return server.ListenAndServe()
}
func serveDecoder() error {
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("MEDIA_WORKER_SECRET") != "" || os.Getenv("DATABASE_URL") != "" {
		return errors.New("decoder must not receive broker secrets")
	}
	if err := cleanupDecoderFiles(); err != nil {
		return err
	}
	worker, err := mediaproc.New(
		mediaproc.Config{
			DecodeOnly:  true,
			FFmpegPath:  "/usr/bin/ffmpeg",
			FFprobePath: "/usr/bin/ffprobe",
			Secret:      mediaproc.DecoderAuthorization,
		},
	)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return err
	}
	directory, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		return err
	}
	const privateDirectoryMode = 0700
	if directory.Mode().Perm() != privateDirectoryMode {
		return errors.New("decoder IPC directory must be private")
	}
	if info, statErr := os.Lstat(socketPath); statErr == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("decoder socket path is not a socket")
		}
		if err = os.Remove(socketPath); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socketPath, 0600); err != nil {
		return err
	}
	server := serverFor(worker)
	return server.Serve(listener)
}

// The decoder owns a private tmpfs; remove jobs left by a terminated process.
func cleanupDecoderFiles() error {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "media-") {
			if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
func serverFor(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":8091",
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       readTimeout,
		MaxHeaderBytes:    maxHeaders,
	}
}

const (
	headerTimeout = 5 * time.Second
	readTimeout   = 30 * time.Second
	writeTimeout  = 160 * time.Second
	maxHeaders    = 8192
)

func mediaApplicationName() (string, error) {
	instance, err := runtimeapp.EnvironmentInstance(os.Getenv("ZNS_ENV") == "production")
	if err != nil {
		return "", err
	}
	if instance == (runtimeapp.Instance{}) {
		return "", nil
	}
	return instance.ApplicationName("media")
}
