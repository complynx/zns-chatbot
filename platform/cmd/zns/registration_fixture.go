package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func registrationFixtureConfig() (sandbox.RegistrationFixture, bool, error) {
	return parseRegistrationFixture(
		os.Getenv("REGISTRATION_FIXTURE_ACTION"),
		os.Getenv("REGISTRATION_FIXTURE_STAND"),
		os.Getenv("REGISTRATION_FIXTURE_OPENS_AT"),
		os.Getenv("REGISTRATION_FIXTURE_CLOCK_ANCHOR"),
	)
}

func parseRegistrationFixture(action, stand, opens, anchor string) (sandbox.RegistrationFixture, bool, error) {
	if action == "" && stand == "" && opens == "" && anchor == "" {
		return sandbox.RegistrationFixture{}, false, nil
	}
	if action == "" || stand != sandbox.RegistrationFixtureStand {
		return sandbox.RegistrationFixture{}, false, errors.New(
			"registration fixture action and exact stand are required",
		)
	}
	f := sandbox.RegistrationFixture{Action: action, Stand: stand}
	switch action {
	case "init":
		var err error
		f.OpensAt, err = time.Parse(time.RFC3339, opens)
		if err != nil {
			return sandbox.RegistrationFixture{}, false, errors.New("registration fixture opening must be RFC3339")
		}
	case "read",
		"revoke-payment-a",
		"restore-payment-a",
		"grant-payment-b",
		"revoke-payment-b",
		"revoke-booking-admin",
		"restore-booking-admin":
		if opens != "" {
			return sandbox.RegistrationFixture{}, false, errors.New("registration fixture opening is init-only")
		}
	default:
		return sandbox.RegistrationFixture{}, false, errors.New("unknown registration fixture action")
	}
	if anchor != "" && action != "init" {
		return sandbox.RegistrationFixture{}, false, errors.New("registration clock anchor is setup-only")
	}
	var err error
	f.ClockAnchor, err = parseRegistrationFixtureClockAnchor(anchor, opens)
	if err != nil {
		return sandbox.RegistrationFixture{}, false, err
	}
	return f, true, nil
}

func parseRegistrationFixtureClockAnchor(anchor, opens string) (time.Time, error) {
	if anchor == "" {
		return time.Time{}, nil
	}
	if len(anchor) > 64 || len(opens) > 64 || !strings.HasSuffix(opens, "Z") {
		return time.Time{}, errors.New("registration clock setup timestamps are invalid")
	}
	opening, err := time.Parse(time.RFC3339Nano, opens)
	if err != nil || opening.Nanosecond()%1000 != 0 {
		return time.Time{}, errors.New("registration clock opening requires UTC microseconds")
	}
	return (registrationclock.Settings{Anchor: anchor}).AnchorTime()
}

func runRegistrationFixture(ctx context.Context, db *pgxpool.Pool, f sandbox.RegistrationFixture) error {
	state, err := sandbox.ApplyRegistrationFixture(ctx, db, f)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}
