package main

import (
	"errors"
	"flag"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
)

// validateStartupModes rejects ambiguous operator commands before opening dependencies.
func validateStartupModes(flags *flag.FlagSet, check, migrate bool,
	telegramID int64, actor, reason string, installation db.InstallationOptions,
) (bool, error) {
	ban, banOptions := false, false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "ban-telegram-user":
			ban = true
		case "ban-actor", "ban-reason", "ban-public-reason":
			banOptions = true
		}
	})
	if flags.NArg() != 0 || check && migrate || ban && (check || migrate) {
		return false, errors.New("unexpected arguments or conflicting startup modes")
	}
	if !ban && banOptions || ban && (telegramID <= 0 || telegramID >= 1<<52 ||
		strings.TrimSpace(reason) == "" || strings.TrimSpace(actor) == "") {
		return false, errors.New("ban requires a positive Telegram ID, operator and reason in its own startup mode")
	}
	_, err := installationOptions(flags, migrate,
		installation.RuntimeRole, installation.DefaultLanguage, installation.MaxParallelTasks)
	return ban, err
}
