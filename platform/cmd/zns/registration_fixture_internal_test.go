package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestRegistrationFixtureOptInParsing(t *testing.T) {
	t.Parallel()
	f, enabled, err := parseRegistrationFixture("", "", "")
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.Equal(t, sandbox.RegistrationFixture{}, f)
	f, enabled, err = parseRegistrationFixture("init", sandbox.RegistrationFixtureStand, "2026-10-10T12:00:00Z")
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 2026, f.OpensAt.Year())
	for _, action := range []string{"read", "revoke-payment-a", "restore-payment-a", "grant-payment-b", "revoke-payment-b", "revoke-booking-admin", "restore-booking-admin"} {
		f, enabled, err = parseRegistrationFixture(action, sandbox.RegistrationFixtureStand, "")
		require.NoError(t, err)
		assert.True(t, enabled)
		assert.Equal(t, action, f.Action)
		_, _, err = parseRegistrationFixture(action, sandbox.RegistrationFixtureStand, "2026-10-10T12:00:00Z")
		require.ErrorContains(t, err, "init-only")
	}
	for _, input := range [][3]string{
		{"", sandbox.RegistrationFixtureStand, ""},
		{"init", sandbox.RegistrationFixtureStand, "invalid"},
		{"init", "another-stand", "2026-10-10T12:00:00Z"},
		{"read", sandbox.RegistrationFixtureStand, "2026-10-10T12:00:00Z"},
		{"grant", sandbox.RegistrationFixtureStand, ""},
	} {
		_, _, err = parseRegistrationFixture(input[0], input[1], input[2])
		require.Error(t, err)
	}
}
