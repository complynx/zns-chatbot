package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func registrationFixtureConfig() (sandbox.RegistrationFixture, bool, error) {
	return parseRegistrationFixture(
		os.Getenv("REGISTRATION_FIXTURE_ACTION"),
		os.Getenv("REGISTRATION_FIXTURE_STAND"),
		os.Getenv("REGISTRATION_FIXTURE_OPENS_AT"),
	)
}

func parseRegistrationFixture(action, stand, opens string) (sandbox.RegistrationFixture, bool, error) {
	if action == "" && stand == "" && opens == "" {
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
	case "read", "revoke-payment-a", "revoke-booking-admin":
		if opens != "" {
			return sandbox.RegistrationFixture{}, false, errors.New("registration fixture opening is init-only")
		}
	default:
		return sandbox.RegistrationFixture{}, false, errors.New("unknown registration fixture action")
	}
	return f, true, nil
}

func runRegistrationFixture(ctx context.Context, db *pgxpool.Pool, f sandbox.RegistrationFixture) error {
	state, err := sandbox.ApplyRegistrationFixture(ctx, db, f)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}
