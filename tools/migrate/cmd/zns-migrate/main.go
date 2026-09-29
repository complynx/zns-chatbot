package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
)

type result struct {
	PrepareIdentities *migrate.IdentityPrepareSummary `json:"prepare_identities,omitempty"`
	Massage           *migrate.MassagePlanSummary     `json:"massage,omitempty"`
	ApplyMassage      *migrate.MassageApplySummary    `json:"apply_massage,omitempty"`
	Food              *migrate.FoodPlanSummary        `json:"food,omitempty"`
	ApplyFood         *migrate.FoodApplySummary       `json:"apply_food,omitempty"`
	Passes            *migrate.PassPlanSummary        `json:"passes,omitempty"`
	ApplyPasses       *migrate.PassApplySummary       `json:"apply_passes,omitempty"`
	ApplyOrders       *migrate.OrderApplySummary      `json:"apply_orders,omitempty"`
	Orders            *migrate.OrderPlanSummary       `json:"orders,omitempty"`
	Events            *migrate.EventPlanSummary       `json:"events,omitempty"`
	ApplyEvents       *migrate.EventApplySummary      `json:"apply_events,omitempty"`
	ApplyUsers        *migrate.UserApplySummary       `json:"apply_users,omitempty"`
	Users             *migrate.UserPlanSummary        `json:"users,omitempty"`
	Report            *migrate.Report                 `json:"report,omitempty"`
	Reused            bool                            `json:"reused"`
	Error             string                          `json:"error,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func run(arguments []string, output io.Writer) int {
	response, err := execute(arguments)
	if err != nil {
		response.Error = err.Error()
	}
	if encodeErr := json.NewEncoder(output).Encode(response); encodeErr != nil {
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}

func execute(arguments []string) (result, error) {
	if len(arguments) > 0 && arguments[0] == "prepare-identities" {
		return executePrepareIdentities(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == "reconcile" {
		return executeReconcile(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == "apply" {
		return executeApply(arguments[1:])
	}
	if len(arguments) > 0 && arguments[0] == "plan" {
		return executePlan(arguments[1:])
	}
	if len(arguments) == 0 || arguments[0] != "verify" && arguments[0] != "stage" {
		return result{}, errors.New("expected_verify_or_stage")
	}
	flags := flag.NewFlagSet("zns-migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	snapshot := flags.String("snapshot", "", "snapshot directory containing manifest.json")
	target := flags.String("out", "", "new staging directory (stage only)")
	limits := migrate.DefaultLimits()
	registerLimits(flags, &limits)
	if flags.Parse(arguments[1:]) != nil || flags.NArg() != 0 || *snapshot == "" {
		return result{}, errors.New("invalid_arguments")
	}
	if arguments[0] == "verify" {
		if *target != "" {
			return result{}, errors.New("verify_does_not_write")
		}
		report, err := migrate.Verify(*snapshot, limits)
		return result{Report: &report}, err
	}
	if *target == "" {
		return result{}, errors.New("stage_target_required")
	}
	report, reused, err := migrate.Stage(*snapshot, *target, limits)
	if err != nil {
		return result{Report: &report}, fmt.Errorf("stage_failed: %w", err)
	}
	return result{Report: &report, Reused: reused}, nil
}
