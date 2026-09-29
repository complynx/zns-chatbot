package config

import (
	"errors"
	"log/slog"
)

func (l Log) SlogLevel() (slog.Level, error) {
	const traceLevel slog.Level = -8
	switch l.Level {
	case "trace":
		return traceLevel, nil
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("configuration log.level must be trace, debug, info, warning, or error")
	}
}
