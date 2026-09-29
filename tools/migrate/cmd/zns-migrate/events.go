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

func executeEventPlan(arguments []string) (result, error) {
	flags := flag.NewFlagSet("plan events", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	target := flags.String("out", "", "private JSON plan file")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *target == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.PlanEvents(*stage, *target, limits)
	return result{Events: &summary, Reused: summary.Reused}, err
}

func executeEventApply(arguments []string) (result, error) {
	flags := flag.NewFlagSet("apply events", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	plan := flags.String("plan", "", "original private event plan")
	resolutions := flags.String("resolutions", "", "private operator policy attestations")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *plan == "" || *resolutions == "" {
		return result{}, errors.New("invalid_arguments")
	}
	dsn := os.Getenv("MIGRATE_DATABASE_URL")
	if dsn == "" {
		return result{}, errors.New("apply_database_url_required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	summary, err := migrate.ApplyEvents(ctx, dsn, *stage, *plan, *resolutions, limits)
	return result{ApplyEvents: &summary}, err
}
