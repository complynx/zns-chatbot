package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

// refreshScriptKnowledge runs after the call result is durable, outside the VM
// callback deadline. Failure leaves the host's refresh marker pending for retry.
func (b *Bot) refreshScriptKnowledge(ctx context.Context, owner string, command knowledge.Command) error {
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return errors.New("tool unavailable")
	}
	ctx, span := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: "script", Operation: "script.knowledge.refresh"},
	)
	err := b.saveKnowledgeView(
		ctx,
		owner,
		knowledgeView{Event: command.Event, Mode: knowledgeCommandMode(command.Name)},
	)
	if err == nil {
		err = b.RenderKnowledge(ctx, owner, source.in.chat)
	}
	span.Finish(err)
	return err
}
