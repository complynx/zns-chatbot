package runtimeapp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func TestInstanceIdentityIsCanonicalAndBounded(t *testing.T) {
	t.Parallel()
	instance := runtimeapp.Instance{Installation: "abcdef012345", Launch: "0123456789abcdef01234567"}
	names := map[string]bool{}
	for _, component := range []string{"app", "admit", "media"} {
		name, err := instance.ApplicationName(component)
		require.NoError(t, err)
		require.Less(t, len(name), 64)
		require.False(t, names[name])
		names[name] = true
	}
	for _, invalid := range []runtimeapp.Instance{
		{}, {Installation: "ABCDEF012345", Launch: instance.Launch},
		{Installation: instance.Installation, Launch: strings.Repeat("0", 23)},
		{Installation: instance.Installation, Launch: strings.Repeat("0", 25)},
		{Installation: instance.Installation, Launch: strings.Repeat("x", 24)},
	} {
		require.ErrorIs(t, invalid.Validate(), runtimeapp.ErrInstance)
	}
	_, err := instance.ApplicationName("unowned")
	require.ErrorIs(t, err, runtimeapp.ErrInstance)
}

func TestEnvironmentInstanceRejectsPartialIdentity(t *testing.T) {
	t.Setenv(runtimeapp.InstallationEnv, "abcdef012345")
	t.Setenv(runtimeapp.LaunchEnv, "")
	_, err := runtimeapp.EnvironmentInstance(false)
	require.ErrorIs(t, err, runtimeapp.ErrInstance)
}
