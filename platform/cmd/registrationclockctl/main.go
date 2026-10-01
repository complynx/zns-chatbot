// registrationclockctl is the sole trusted writer for the allocated synthetic
// case clock. No model, fake-provider or ordinary user can invoke this control.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

const operatorTimeout = 30 * time.Second
const operatorConfigBytes = 1 << 20

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "registration clock operation failed:", err)
		os.Exit(1)
	}
}

func run() error {
	action := flag.String("action", "", "init, read or advance")
	revision := flag.Uint64("expected-revision", 0, "current observed revision for advance")
	target := flag.String("target", "", "strictly later UTC RFC3339 microsecond instant for advance")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected operator arguments")
	}
	change := sandbox.RegistrationClockChange{Action: *action, ExpectedRevision: *revision}
	if *target != "" {
		var err error
		change.Target, err = time.Parse(time.RFC3339Nano, *target)
		if err != nil || !strings.HasSuffix(*target, "Z") || change.Target.Nanosecond()%1000 != 0 {
			return errors.New("invalid registration clock target")
		}
	}
	settings := registrationclock.Settings{
		File: os.Getenv("REGISTRATION_CLOCK_FILE"), Installation: os.Getenv("REGISTRATION_CLOCK_INSTALLATION"),
		Case: os.Getenv("REGISTRATION_CLOCK_CASE"), DatabaseAddress: os.Getenv("REGISTRATION_CLOCK_DATABASE_ADDRESS"),
		Anchor: os.Getenv("REGISTRATION_CLOCK_ANCHOR"),
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	var data []byte
	if path := os.Getenv("ZNS_CONFIG_FILE"); path != "" {
		root, err := os.OpenRoot(filepath.Dir(path))
		if err != nil {
			return errors.New("operator configuration directory unavailable")
		}
		defer root.Close()
		file, err := root.Open(filepath.Base(path))
		if err != nil {
			return errors.New("operator configuration unavailable")
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, operatorConfigBytes+1))
		if err != nil {
			return errors.New("operator configuration unreadable")
		}
	}
	cfg, err := config.Load("fixture", data, os.Environ())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), operatorTimeout)
	defer cancel()
	db, err := pgxpool.New(ctx, cfg.Database.URL.Value())
	if err != nil {
		return errors.New("operator database configuration invalid")
	}
	defer db.Close()
	state, err := sandbox.ApplyRegistrationClock(ctx, db, cfg, settings, change)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}
