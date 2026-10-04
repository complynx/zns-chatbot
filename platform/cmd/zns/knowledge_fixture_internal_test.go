package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestKnowledgeFixtureOptInParsing(t *testing.T) {
	t.Parallel()
	f, enabled, err := parseKnowledgeFixture("", "", "", "")
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.Equal(t, sandbox.KnowledgeFixture{}, f)
	for _, scope := range []string{
		"", "sandbox-festival", "sandbox-past", sandbox.RegistrationFixtureEventA, sandbox.RegistrationFixtureEventB,
	} {
		f, enabled, err = parseKnowledgeFixture("read", sandbox.RegistrationFixtureStand, scope, "review")
		require.NoError(t, err)
		assert.True(t, enabled)
		assert.Equal(t, scope, f.Scope)
	}
	for _, value := range [][4]string{
		{"", sandbox.RegistrationFixtureStand, "", "review"},
		{"read", "", "", "review"},
		{"read", sandbox.RegistrationFixtureStand, "", ""},
		{"init", sandbox.RegistrationFixtureStand, "", "review"},
		{"grant", "production", "", "review"},
		{"revoke", sandbox.RegistrationFixtureStand, "other", "review"},
		{"read", sandbox.RegistrationFixtureStand, "", "admin"},
	} {
		_, enabled, err = parseKnowledgeFixture(value[0], value[1], value[2], value[3])
		assert.True(t, enabled)
		require.Error(t, err)
	}
}

func TestKnowledgeFixtureDispatchRejectsCombinedControls(t *testing.T) {
	t.Setenv("KNOWLEDGE_FIXTURE_ACTION", "read")
	t.Setenv("KNOWLEDGE_FIXTURE_STAND", sandbox.RegistrationFixtureStand)
	t.Setenv("KNOWLEDGE_FIXTURE_SCOPE", "")
	t.Setenv("KNOWLEDGE_FIXTURE_PERMISSION", "review")
	t.Setenv("REGISTRATION_FIXTURE_ACTION", "read")
	t.Setenv("REGISTRATION_FIXTURE_STAND", sandbox.RegistrationFixtureStand)
	t.Setenv("REGISTRATION_FIXTURE_OPENS_AT", "")
	t.Setenv("REGISTRATION_FIXTURE_CLOCK_ANCHOR", "")
	require.ErrorContains(t, runFixture(t.Context(), nil, "product-fixture"), "cannot be combined")
	t.Setenv("KNOWLEDGE_FIXTURE_ACTION", "init")
	require.ErrorContains(t, runFixture(t.Context(), nil, "product-fixture"), "unknown knowledge fixture")
}
