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

func executeMessagePlan(arguments []string) (result, error) {
	flags := flag.NewFlagSet("plan messages", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified stage")
	out := flags.String("out", "", "private plan")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *out == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.PlanMessages(*stage, *out, limits)
	return result{Messages: &summary, Reused: summary.Reused}, err
}
func executeMessageValidation(arguments []string) (result, error) {
	flags := flag.NewFlagSet("validate messages", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified stage")
	plan := flags.String("plan", "", "private plan")
	resolutions := flags.String("resolutions", "", "private reviewed decisions")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *plan == "" || *resolutions == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.ValidateMessages(*stage, *plan, *resolutions, limits)
	return result{ValidateMessages: &summary}, err
}

func executeMessageImport(arguments []string, verifyOnly bool) (result, error) {
	flags := flag.NewFlagSet("apply messages", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified stage")
	plan := flags.String("plan", "", "private plan")
	resolutions := flags.String("resolutions", "", "private reviewed decisions")
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
	var summary migrate.MessageApplySummary
	var err error
	if verifyOnly {
		summary, err = migrate.ReconcileMessages(ctx, dsn, *stage, *plan, *resolutions, limits)
	} else {
		summary, err = migrate.ApplyMessages(ctx, dsn, *stage, *plan, *resolutions, limits)
	}
	return result{ApplyMessages: &summary}, err
}
func executeMessages(arguments []string) (result, error) {
	switch arguments[0] {
	case "plan":
		return executeMessagePlan(arguments[2:])
	case "validate":
		return executeMessageValidation(arguments[2:])
	case "apply":
		return executeMessageImport(arguments[2:], false)
	case "reconcile":
		return executeMessageImport(arguments[2:], true)
	default:
		return result{}, errors.New("invalid_arguments")
	}
}
