package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

type configuration struct {
	Installation string   `json:"installation"`
	Directory    string   `json:"state_directory"`
	Project      string   `json:"compose_project"`
	ComposeFiles []string `json:"compose_files"`
	ManagedRoles []string `json:"managed_roles"`
	DatabaseHost string   `json:"runtime_database_host"`
}

const commandArgumentCount = 2
const inventoryConnectTimeout = 10 * time.Second
const launchHexLength = 24

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "replacement blocked or runtime stopped")
		os.Exit(1)
	}
}

func run() (result error) {
	if len(os.Args) != commandArgumentCount {
		return replacement.ErrConfiguration
	}
	cfg, err := readConfiguration(os.Args[1])
	if err != nil {
		return err
	}
	unlock, err := replacement.Lock(cfg.Directory)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, unlock()) }()
	host, err := os.ReadFile("/etc/machine-id")
	if err != nil || strings.TrimSpace(string(host)) == "" {
		return replacement.ErrConfiguration
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	connect, stop := context.WithTimeout(ctx, inventoryConnectTimeout)
	conn, err := pgx.Connect(connect, os.Getenv("ZNS_INVENTORY_DATABASE_URL"))
	stop()
	if err != nil {
		return errors.New("database inventory connection unavailable")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		result = errors.Join(result, conn.Close(cleanup))
	}()
	coordinator := replacement.Coordinator{
		Engine: replacement.Docker{
			Command:      replacement.DockerCommand{},
			Installation: cfg.Installation,
			Project:      cfg.Project,
			Files:        cfg.ComposeFiles,
			ManagedRoles: cfg.ManagedRoles,
			Database:     conn.Config().Database,
			DatabaseHost: cfg.DatabaseHost,
		},
		Sessions:     replacement.PostgresSessions{Conn: conn, Roles: cfg.ManagedRoles},
		Journal:      replacement.FileJournal{Directory: cfg.Directory},
		Installation: cfg.Installation, Host: strings.TrimSpace(string(host)), NewLaunch: newLaunch,
	}
	return coordinator.Run(ctx)
}

func readConfiguration(path string) (configuration, error) {
	if !filepath.IsAbs(path) {
		return configuration{}, replacement.ErrConfiguration
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return configuration{}, replacement.ErrConfiguration
	}
	defer root.Close()
	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return configuration{}, replacement.ErrConfiguration
	}
	var cfg configuration
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, replacement.ErrConfiguration
	}
	instance := runtimeapp.Instance{Installation: cfg.Installation, Launch: strings.Repeat("0", launchHexLength)}
	if instance.Validate() != nil || !filepath.IsAbs(cfg.Directory) || cfg.Project == "" ||
		len(cfg.ComposeFiles) == 0 || len(cfg.ManagedRoles) == 0 || cfg.DatabaseHost == "" {
		return cfg, replacement.ErrConfiguration
	}
	for _, file := range cfg.ComposeFiles {
		if !filepath.IsAbs(file) {
			return cfg, replacement.ErrConfiguration
		}
	}
	if slices.Contains(cfg.ManagedRoles, "") {
		return cfg, replacement.ErrConfiguration
	}
	return cfg, nil
}

func newLaunch() (string, error) {
	const bytes = 12
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
