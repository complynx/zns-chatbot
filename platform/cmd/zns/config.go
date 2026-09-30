package main

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func loadConfig(command string) (config.Config, error) {
	const maxConfigBytes = 1048576
	var data []byte
	if path := os.Getenv("ZNS_CONFIG_FILE"); path != "" {
		//nolint:gosec // Operator-supplied startup path; no Telegram or API input can select it.
		file, err := os.Open(path)
		if err != nil {
			return config.Config{}, errors.New("configuration file unavailable")
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
		if err != nil {
			return config.Config{}, errors.New("configuration file unreadable")
		}
	}
	return config.Load(command, data, configEnvironment(os.Environ()))
}

// configEnvironment returns a copy without the lifecycle identity that runtimeapp
// reads and validates from the process environment; the process is not modified.
func configEnvironment(environ []string) []string {
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if name == runtimeapp.InstallationEnv || name == runtimeapp.LaunchEnv {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
