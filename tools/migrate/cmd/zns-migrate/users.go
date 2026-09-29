package main

import (
	"errors"
	"flag"
	"io"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
)

func executeUserPlan(arguments []string) (result, error) {
	if len(arguments) == 0 || arguments[0] != "users" {
		return result{}, errors.New("expected_plan_users")
	}
	flags := flag.NewFlagSet("plan users", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	target := flags.String("out", "", "private JSONL plan file")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments[1:]) != nil || flags.NArg() != 0 || *stage == "" || *target == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.PlanUsers(*stage, *target, limits)
	return result{Users: &summary, Reused: summary.Reused}, err
}
