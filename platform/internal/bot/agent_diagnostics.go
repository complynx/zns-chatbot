package bot

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

const diagnosticContextKind = "diagnostic_context"
const diagnosticModelPhase = "model"

func (b *Bot) startAgentDiagnostics(
	ctx context.Context,
	owner string,
	updateID int64,
) (context.Context, *observability.AgentSpan) {
	if b.Logger == nil {
		return ctx, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var correlation string
	var attempt uint32
	err := b.DB.QueryRow(callCtx, `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES($1,$2,$3,jsonb_build_object('correlation',$4::text,'attempt',1))
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=jsonb_set(bot.interactions.content,'{attempt}',
 to_jsonb((bot.interactions.content->>'attempt')::bigint+1))
 RETURNING content->>'correlation',(content->>'attempt')::bigint`, owner, updateID, diagnosticContextKind, uuid.NewString()).
		Scan(&correlation, &attempt)
	if err != nil {
		b.Logger.WarnContext(ctx, "agent diagnostics omitted")
		return ctx, nil
	}
	recorder, err := observability.NewAgentEvents(b.Logger, correlation, attempt)
	if err != nil {
		b.Logger.WarnContext(ctx, "agent diagnostics omitted")
		return ctx, nil
	}
	return observability.StartAgentEvent(observability.WithAgentEvents(ctx, recorder),
		observability.AgentEvent{Phase: "request", Operation: "telegram.update"})
}

func (b *Bot) diagnosticPlan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	ctx, span := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: diagnosticModelPhase, Operation: "model.loop"},
	)
	plan, err := b.Model.Plan(ctx, input)
	if err == nil {
		err = agent.Validate(plan)
		if err != nil {
			span.Outcome("invalid", "invalid_plan")
		}
	}
	span.Finish(err)
	return plan, err
}

func diagnosticPlanReplay(ctx context.Context) {
	observability.EmitAgentEvent(
		ctx,
		observability.AgentEvent{
			Phase:     diagnosticModelPhase,
			Operation: "model.cache",
			Outcome:   "replayed",
			Replay:    true,
		},
	)
}
