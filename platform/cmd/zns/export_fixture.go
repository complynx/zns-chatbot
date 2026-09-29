package main

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func exportFixtureConfig() (sandbox.ExportFixture, error) {
	fixture := sandbox.ExportFixture{
		OrderID:  os.Getenv("EXPORT_FIXTURE_ORDER_ID"),
		BatchTag: os.Getenv("EXPORT_FIXTURE_BATCH_TAG"),
	}
	if raw := os.Getenv("EXPORT_FIXTURE_CHOICE"); raw != "" {
		const maxFixtureChoiceBytes = 256 << 10
		if len(raw) > maxFixtureChoiceBytes {
			return fixture, errors.New("fixture choice exceeds 256 KiB")
		}
		fixture.Choice = &orders.Choice{}
		if err := json.Unmarshal([]byte(raw), fixture.Choice); err != nil {
			return fixture, err
		}
	}
	if value := os.Getenv("EXPORT_FIXTURE_ADMIN_ENABLED"); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fixture, err
		}
		fixture.AdminEnabled = &enabled
	}
	if value := os.Getenv("EXPORT_FIXTURE_BATCH_COUNT"); value != "" || fixture.BatchTag != "" {
		count, err := strconv.Atoi(value)
		if err != nil {
			return fixture, err
		}
		fixture.BatchCount = count
	}
	return fixture, nil
}
