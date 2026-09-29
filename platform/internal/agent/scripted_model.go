package agent

import "context"

// Scripted is a deterministic fixture for the single-process synthetic stand.
type Scripted struct{}

func (Scripted) Plan(ctx context.Context, input Input) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	return scriptedPlan(input), nil
}
