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

func executeFoodPlan(arguments []string) (result, error) {
	flags := flag.NewFlagSet("plan food", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	target := flags.String("out", "", "private food plan file")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *target == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.PlanFood(*stage, *target, limits)
	return result{Food: &summary, Reused: summary.Reused}, err
}

func executeFoodImport(arguments []string, verify bool) (result, error) {
	input, err := parseImportFlags(arguments)
	if err != nil {
		return result{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.FoodApplySummary
	if verify {
		summary, err = migrate.ReconcileFood(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	} else {
		summary, err = migrate.ApplyFood(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	}
	return result{ApplyFood: &summary}, err
}

const foodDomainName = "food"
