package agent

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

// observeStructured records each provider invocation after authorization and
// projection. The surrounding logical planning loop is a separate operation.
func observeStructured(ctx context.Context, prompt providerPrompt, call structuredCall) (string, error) {
	operation := "model.plan"
	if prompt.name == selectionName {
		operation = "model.skills"
	}
	ctx, span := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: diagnosticModelPhase, Operation: operation, InputBytes: len(prompt.input)},
	)
	text, err := call(ctx, prompt)
	span.Result(len(text), 0, false)
	span.Finish(err)
	return text, err
}

const diagnosticModelPhase = "model"

func recordPlanValidation(ctx context.Context, err error) {
	if err != nil {
		observability.EmitAgentEvent(ctx, observability.AgentEvent{Phase: diagnosticModelPhase,
			Operation: "model.validation", Outcome: "invalid", ErrorCode: "invalid_plan"})
	}
}
