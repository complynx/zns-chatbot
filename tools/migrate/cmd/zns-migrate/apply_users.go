package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
)

func executeUserApply(arguments []string) (result, error) {
	return executeUserImport(arguments, false)
}

func executeUserImport(arguments []string, verify bool) (result, error) {
	if len(arguments) == 0 || arguments[0] != "users" {
		return result{}, errors.New("expected_apply_users")
	}
	flags := flag.NewFlagSet("apply users", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	plan := flags.String("plan", "", "original private users plan")
	resolutions := flags.String("resolutions", "", "private operator-attested identity and eligibility resolutions")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments[1:]) != nil || flags.NArg() != 0 || *stage == "" || *plan == "" || *resolutions == "" {
		return result{}, errors.New("invalid_arguments")
	}
	dsn := os.Getenv("MIGRATE_DATABASE_URL")
	if dsn == "" {
		return result{}, errors.New("apply_database_url_required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.UserApplySummary
	var err error
	if verify {
		summary, err = migrate.ReconcileUsers(ctx, dsn, *stage, *plan, *resolutions, limits)
	} else {
		summary, err = migrate.ApplyUsers(ctx, dsn, *stage, *plan, *resolutions, limits)
	}
	return result{ApplyUsers: &summary}, err
}
