package main

import (
	"errors"
	"io"
	"os"

	"github.com/complynx/zns-chatbot/platform/internal/config"
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
	return config.Load(command, data, os.Environ())
}
