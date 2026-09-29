package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

func (s Service) ExecuteWorkflow(
	ctx context.Context,
	actor string,
	action workflow.Action,
	source readsource.Derivation,
) (workflow.Workflow, error) {
	if !source.Valid() || action.Origin != agentOrigin {
		return workflow.Workflow{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.beginSourceMutation(ctx, []string{actor}, source)
	if err != nil {
		return workflow.Workflow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Workflow.PrepareWorkflowInTx(ctx, tx, actor, action)
	if err != nil {
		return workflow.Workflow{}, err
	}
	return commitPrepared(ctx, tx, actor, source, prepared)
}
