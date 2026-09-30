package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const (
	runtimeIdentityInstallation = "abcdef012345"
	runtimeIdentityLaunch       = "0123456789abcdef01234567"
)

// runtimeIdentityAppEnvironment mirrors the valid sandbox app fixture used by config tests.
func runtimeIdentityAppEnvironment() []string {
	return []string{
		"ZNS_ENV=sandbox", "DATABASE_URL=postgres://localhost/test", "SANDBOX_SIGNING_KEY=" + strings.Repeat("k", 32),
		"CORE_URL=http://core:8080", "MODEL_URL=http://model:8080", "MODEL_PROVIDER=scripted", "ZNS_CORE__URL=",
	}
}

func setRuntimeIdentityAppEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("ZNS_CONFIG_FILE", "")
	for _, entry := range runtimeIdentityAppEnvironment() {
		name, value, _ := strings.Cut(entry, "=")
		t.Setenv(name, value)
	}
}

func TestConfigEnvironmentOmitsOnlyRuntimeIdentity(t *testing.T) {
	t.Parallel()
	tags := []string{
		runtimeapp.InstallationEnv + "=" + runtimeIdentityInstallation,
		runtimeapp.LaunchEnv + "=" + runtimeIdentityLaunch,
	}
	environ := append(runtimeIdentityAppEnvironment(), tags...)
	original := slices.Clone(environ)

	filtered := configEnvironment(environ)
	require.Equal(t, runtimeIdentityAppEnvironment(), filtered)
	require.Equal(t, original, environ, "caller environment copy must stay intact")
	_, err := config.Load("app", nil, environ)
	require.ErrorContains(t, err, "unknown configuration environment variable")
	_, err = config.Load("app", nil, filtered)
	require.NoError(t, err)

	for _, entry := range []string{
		"ZNS_UNKNOWN=value",
		runtimeapp.InstallationEnv + "_EXTRA=" + runtimeIdentityInstallation,
		runtimeapp.LaunchEnv + "X=" + runtimeIdentityLaunch,
		"ZNS_INSTALLATION=" + runtimeIdentityInstallation,
	} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			_, loadErr := config.Load("app", nil, configEnvironment(append(slices.Clone(environ), entry)))
			require.ErrorContains(t, loadErr, "unknown configuration environment variable")
		})
	}
}

func TestLoadConfigAdmitsRuntimeIdentityWithoutConsumingIt(t *testing.T) {
	setRuntimeIdentityAppEnvironment(t)
	t.Setenv(runtimeapp.InstallationEnv, runtimeIdentityInstallation)
	t.Setenv(runtimeapp.LaunchEnv, runtimeIdentityLaunch)

	_, err := loadConfig("app")
	require.NoError(t, err)

	installation, present := os.LookupEnv(runtimeapp.InstallationEnv)
	require.True(t, present)
	require.Equal(t, runtimeIdentityInstallation, installation)
	launch, present := os.LookupEnv(runtimeapp.LaunchEnv)
	require.True(t, present)
	require.Equal(t, runtimeIdentityLaunch, launch)
	instance, err := runtimeapp.EnvironmentInstance(true)
	require.NoError(t, err)
	require.Equal(t, runtimeIdentityInstallation, instance.Installation)
	require.Equal(t, runtimeIdentityLaunch, instance.Launch)
}

func TestLoadConfigKeepsUnknownZNSStrict(t *testing.T) {
	setRuntimeIdentityAppEnvironment(t)
	t.Setenv(runtimeapp.InstallationEnv, runtimeIdentityInstallation)
	t.Setenv(runtimeapp.LaunchEnv, runtimeIdentityLaunch)
	t.Setenv("ZNS_UNKNOWN_RUNTIME_SETTING", "value")

	_, err := loadConfig("app")
	require.ErrorContains(t, err, "unknown configuration environment variable ZNS_UNKNOWN_RUNTIME_SETTING")
}

func TestRuntimeIdentityValidatedAfterConfigFiltering(t *testing.T) {
	upper := strings.ToUpper(runtimeIdentityInstallation)
	for name, tagged := range map[string]runtimeapp.Instance{
		"installation only": {Installation: runtimeIdentityInstallation},
		"launch only":       {Launch: runtimeIdentityLaunch},
		"uppercase":         {Installation: upper, Launch: runtimeIdentityLaunch},
		"short launch":      {Installation: runtimeIdentityInstallation, Launch: runtimeIdentityLaunch[1:]},
		"non-hex":           {Installation: runtimeIdentityInstallation, Launch: strings.Repeat("x", 24)},
	} {
		t.Run(name, func(t *testing.T) {
			setRuntimeIdentityAppEnvironment(t)
			t.Setenv(runtimeapp.InstallationEnv, tagged.Installation)
			t.Setenv(runtimeapp.LaunchEnv, tagged.Launch)

			_, err := loadConfig("app")
			require.NoError(t, err, "config loader leaves runtime identity to runtimeapp")
			_, err = runtimeapp.EnvironmentInstance(false)
			require.ErrorIs(t, err, runtimeapp.ErrInstance)
			_, err = runtimeapp.EnvironmentInstance(true)
			require.ErrorIs(t, err, runtimeapp.ErrInstance)
		})
	}
}
