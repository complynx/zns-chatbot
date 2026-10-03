package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func runFixture(ctx context.Context, db *pgxpool.Pool, command string) error {
	switch command {
	case "product-fixture":
		knowledgeFixture, knowledgeEnabled, knowledgeErr := knowledgeFixtureConfig()
		if knowledgeErr != nil {
			return knowledgeErr
		}
		fixture, enabled, err := registrationFixtureConfig()
		if err != nil {
			return err
		}
		if knowledgeEnabled {
			if enabled {
				return errors.New("knowledge and registration fixture controls cannot be combined")
			}
			return runKnowledgeFixture(ctx, db, knowledgeFixture)
		}
		if enabled {
			return runRegistrationFixture(ctx, db, fixture)
		}
		return sandbox.ApplyProductFixture(ctx, db)
	case "migrate":
		if err := store.Migrate(ctx, db); err != nil {
			return err
		}
		return store.Seed(ctx, db)
	case "export-fixture":
		fixture, err := exportFixtureConfig()
		if err != nil {
			return err
		}
		return sandbox.ApplyExportFixture(ctx, db, fixture)
	case fixtureMode:
		fixture, err := orderFixtureConfig()
		if err != nil {
			return err
		}
		if err = sandbox.ApplyOrderFixture(ctx, db, fixture); err != nil {
			return err
		}
		_, err = (orders.Service{DB: db}).QueueDueReminders(ctx, orders.DefaultReminderAfter)
		return err
	default:
		return errors.New("unknown fixture command")
	}
}

func orderFixtureConfig() (sandbox.OrderFixture, error) {
	fixture := sandbox.OrderFixture{
		OrderID:      os.Getenv("ORDER_FIXTURE_ID"),
		AdminCountry: os.Getenv("ORDER_FIXTURE_ADMIN_COUNTRY"),
	}
	var err error
	if value := os.Getenv("ORDER_FIXTURE_AGE"); value != "" {
		fixture.Age, err = time.ParseDuration(value)
		if err != nil {
			return fixture, err
		}
	}
	if value := os.Getenv("ORDER_FIXTURE_DEADLINE"); value != "" {
		fixture.Deadline, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return fixture, err
		}
	}
	extra, remaining := os.Getenv("ORDER_FIXTURE_EXTRA"), os.Getenv("ORDER_FIXTURE_REMAINING")
	if extra != "" || remaining != "" {
		capacity := &sandbox.CapacityFixture{Extra: extra, Reset: remaining == "default"}
		if !capacity.Reset {
			capacity.Remaining, err = strconv.Atoi(remaining)
			if err != nil {
				return fixture, err
			}
		}
		fixture.Capacity = capacity
	}
	return fixture, nil
}
