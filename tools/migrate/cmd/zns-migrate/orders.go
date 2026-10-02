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

const ordersDomainName = "orders"

func executeApply(arguments []string) (result, error) {
	if len(arguments) > 0 && arguments[0] == massageDomainName {
		return executeMassageImport(arguments[1:], false)
	}
	if len(arguments) > 0 && arguments[0] == foodDomainName {
		return executeFoodImport(arguments[1:], false)
	}
	if len(arguments) > 0 && arguments[0] == passesDomainName {
		return executePassImport(arguments[1:], false)
	}
	if len(arguments) > 0 && arguments[0] == ordersDomainName {
		return executeOrderApply(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == eventsDomainName {
		return executeEventApply(arguments[1:])
	}
	return executeUserApply(arguments)
}
func executeOrderApply(arguments []string) (result, error) {
	return executeOrderImport(arguments, false)
}
func executeOrderImport(arguments []string, verifyOnly bool) (result, error) {
	input, err := parseImportFlags(arguments)
	if err != nil {
		return result{}, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var summary migrate.OrderApplySummary
	if verifyOnly {
		summary, err = migrate.ReconcileOrders(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	} else {
		summary, err = migrate.ApplyOrders(ctx, input.dsn, input.stage, input.plan, input.resolutions, input.limits)
	}
	return result{ApplyOrders: &summary}, err
}

type importFlags struct {
	dsn, stage, plan, resolutions string
	limits                        migrate.Limits
}

func parseImportFlags(arguments []string) (importFlags, error) {
	input := importFlags{dsn: os.Getenv("MIGRATE_DATABASE_URL"), limits: migrate.DefaultLimits()}
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&input.stage, "stage", "", "verified staging directory")
	flags.StringVar(&input.plan, "plan", "", "private order plan")
	flags.StringVar(&input.resolutions, "resolutions", "", "private attestations")
	registerLimits(flags, &input.limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || input.stage == "" || input.plan == "" ||
		input.resolutions == "" {
		return input, errors.New("invalid_arguments")
	}
	if input.dsn == "" {
		return input, errors.New("apply_database_url_required")
	}
	return input, nil
}

func executeReconcile(arguments []string) (result, error) {
	if len(arguments) > 0 && arguments[0] == identityUsersCommand {
		return executeUserImport(arguments, true)
	}
	if len(arguments) > 0 && arguments[0] == eventsDomainName {
		return executeEventImport(arguments[1:], true)
	}
	if len(arguments) > 0 && arguments[0] == massageDomainName {
		return executeMassageImport(arguments[1:], true)
	}
	if len(arguments) > 0 && arguments[0] == foodDomainName {
		return executeFoodImport(arguments[1:], true)
	}
	if len(arguments) > 0 && arguments[0] == passesDomainName {
		return executePassImport(arguments[1:], true)
	}
	if len(arguments) > 0 && arguments[0] == ordersDomainName {
		return executeOrderImport(arguments[1:], true)
	}
	return result{}, errors.New("expected_reconcile_orders")
}

func executePlan(arguments []string) (result, error) {
	if len(arguments) > 0 && arguments[0] == massageDomainName {
		return executeMassagePlan(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == foodDomainName {
		return executeFoodPlan(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == passesDomainName {
		return executePassPlan(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == ordersDomainName {
		return executeOrderPlan(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == "events" {
		return executeEventPlan(arguments[1:])
	}
	return executeUserPlan(arguments)
}

func executeOrderPlan(arguments []string) (result, error) {
	flags := flag.NewFlagSet("plan orders", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stage := flags.String("stage", "", "verified staging directory")
	target := flags.String("out", "", "private prerequisite plan file")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *stage == "" || *target == "" {
		return result{}, errors.New("invalid_arguments")
	}
	summary, err := migrate.PlanOrders(*stage, *target, limits)
	return result{Orders: &summary, Reused: summary.Reused}, err
}
