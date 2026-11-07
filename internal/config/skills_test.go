package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestLoomSkillsEnvironmentOverride(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv("SHUTTLE_LOOM_SKILLS_ENABLED", enabled)
			loaded, err := config.Load(configFile(t, ""))
			require.NoError(t, err)
			require.Equal(t, enabled == "true", loaded.Loom.SkillsEnabled)
		})
	}
	t.Setenv("SHUTTLE_LOOM_SKILLS_ENABLED", "private-invalid-value")
	_, err := config.Load(configFile(t, ""))
	require.EqualError(t, err, "invalid SHUTTLE_LOOM_SKILLS_ENABLED")
}

func TestLoomSkillsYAMLIsOptionalAndTyped(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "true", "false", "invalid-value"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile("../../config/shuttle.example.yaml")
			require.NoError(t, err)
			data = []byte(strings.ReplaceAll(string(data), "\r\n", "\n"))
			if value != "" {
				data = []byte(strings.Replace(string(data), "loom:\n", "loom:\n  skills_enabled: "+value+"\n", 1))
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, data, 0600))
			loaded, err := config.Load(path)
			if value == "invalid-value" {
				require.EqualError(t, err, "invalid startup configuration YAML")
				return
			}
			require.NoError(t, err)
			require.Equal(t, value == "true", loaded.Loom.SkillsEnabled)
		})
	}
}
