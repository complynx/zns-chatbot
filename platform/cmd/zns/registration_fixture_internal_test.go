package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestRegistrationFixtureOptInParsing(t *testing.T) {
	t.Parallel()
	f, enabled, err := parseRegistrationFixture("", "", "", "")
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.Equal(t, sandbox.RegistrationFixture{}, f)
	f, enabled, err = parseRegistrationFixture("init", sandbox.RegistrationFixtureStand, "2026-10-10T12:00:00Z", "")
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 2026, f.OpensAt.Year())
	for _, action := range []string{"read", "revoke-payment-a", "restore-payment-a", "grant-payment-b", "revoke-payment-b", "revoke-booking-admin", "restore-booking-admin"} {
		f, enabled, err = parseRegistrationFixture(action, sandbox.RegistrationFixtureStand, "", "")
		require.NoError(t, err)
		assert.True(t, enabled)
		assert.Equal(t, action, f.Action)
		_, _, err = parseRegistrationFixture(action, sandbox.RegistrationFixtureStand, "2026-10-10T12:00:00Z", "")
		require.ErrorContains(t, err, "init-only")
	}
	for _, input := range [][3]string{
		{"", sandbox.RegistrationFixtureStand, ""},
		{"init", sandbox.RegistrationFixtureStand, "invalid"},
		{"init", "another-stand", "2026-10-10T12:00:00Z"},
		{"read", sandbox.RegistrationFixtureStand, "2026-10-10T12:00:00Z"},
		{"grant", sandbox.RegistrationFixtureStand, ""},
	} {
		_, _, err = parseRegistrationFixture(input[0], input[1], input[2], "")
		require.Error(t, err)
	}
}

func TestRegistrationFixtureClockSetupParsing(t *testing.T) {
	t.Parallel()
	anchor := "2026-10-01T12:00:00.123456Z"
	opening := "2026-10-01T15:17:00.123456Z"
	f, enabled, err := parseRegistrationFixture("init", sandbox.RegistrationFixtureStand, opening, anchor)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 3*time.Hour+17*time.Minute, f.OpensAt.Sub(f.ClockAnchor))
	for _, input := range [][4]string{
		{"init", sandbox.RegistrationFixtureStand, opening, "invalid"},
		{"init", sandbox.RegistrationFixtureStand, opening, "2026-10-01T12:00:00.1234567Z"},
		{"init", sandbox.RegistrationFixtureStand, opening, "2026-10-01T12:00:00+00:00"},
		{"init", sandbox.RegistrationFixtureStand, "2026-10-01T15:17:00+00:00", anchor},
		{"init", sandbox.RegistrationFixtureStand, "2026-10-01T15:17:00.1234567Z", anchor},
		{"init", sandbox.RegistrationFixtureStand, opening, strings.Repeat("1", 65)},
		{"init", sandbox.RegistrationFixtureStand, strings.Repeat("1", 65), anchor},
		{"", sandbox.RegistrationFixtureStand, opening, anchor},
		{"init", "another-stand", opening, anchor},
		{"read", sandbox.RegistrationFixtureStand, "", anchor},
	} {
		_, _, err = parseRegistrationFixture(input[0], input[1], input[2], input[3])
		require.Error(t, err)
	}
}
