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

const eventsDomainName = "events"

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
	return executeEventImport(arguments, false)
}

func executeEventImport(arguments []string, verify bool) (result, error) {
	input, err := parseImportFlags(arguments)
	if err != nil {
		return result{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.EventApplySummary
	if verify {
		summary, err = migrate.ReconcileEvents(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	} else {
		summary, err = migrate.ApplyEvents(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	}
	return result{ApplyEvents: &summary}, err
}
