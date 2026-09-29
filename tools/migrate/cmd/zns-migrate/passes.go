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

const passesDomainName = "passes"

func executePassPlan(arguments []string) (result, error) {
	flags := flag.NewFlagSet("plan passes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	out := flags.String("out", "", "private pass plan")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *out == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.PlanPasses(*stage, *out, limits)
	return result{Passes: &summary, Reused: summary.Reused}, err
}
func executePassImport(arguments []string, verifyOnly bool) (result, error) {
	input, err := parseOrderImportFlags(arguments)
	if err != nil {
		return result{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.PassApplySummary
	if verifyOnly {
		summary, err = migrate.ReconcilePasses(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	} else {
		summary, err = migrate.ApplyPasses(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	}
	return result{ApplyPasses: &summary}, err
}
