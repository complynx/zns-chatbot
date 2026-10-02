package main

import (
	"context"
	"errors"
	"os"
	"os/signal"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
)

func executeUserApply(arguments []string) (result, error) {
	return executeUserImport(arguments, false)
}

func executeUserImport(arguments []string, verify bool) (result, error) {
	if len(arguments) == 0 || arguments[0] != identityUsersCommand {
		return result{}, errors.New("expected_apply_users")
	}
	input, err := parseImportFlags(arguments[1:])
	if err != nil {
		return result{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.UserApplySummary
	if verify {
		summary, err = migrate.ReconcileUsers(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	} else {
		summary, err = migrate.ApplyUsers(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	}
	return result{ApplyUsers: &summary}, err
}
