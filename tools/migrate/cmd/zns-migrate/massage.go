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

const massageDomainName = "massage"

func executeMassagePlan(arguments []string) (result, error) {
	stage, target, limits, err := parseMassagePlanFlags(arguments)
	if err != nil {
		return result{}, err
	}
	summary, err := migrate.PlanMassage(stage, target, limits)
	return result{Massage: &summary, Reused: summary.Reused}, err
}
func executeMassageImport(arguments []string, verify bool) (result, error) {
	input, err := parseOrderImportFlags(arguments)
	if err != nil {
		return result{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.MassageApplySummary
	if verify {
		summary, err = migrate.ReconcileMassage(
			ctx,
			input.dsn,
			input.stage,
			input.plan,
			input.resolutions,
			input.limits,
		)
	} else {
		summary, err = migrate.ApplyMassage(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	}
	return result{ApplyMassage: &summary}, err
}

func parseMassagePlanFlags(arguments []string) (string, string, migrate.Limits, error) {
	flags := flag.NewFlagSet("plan massage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	target := flags.String("out", "", "private massage plan file")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *target == "" {
		return "", "", limits, errors.New("invalid_arguments")
	}

	return *stage, *target, limits, nil
}
