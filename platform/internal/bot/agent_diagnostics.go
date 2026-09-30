package bot

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

const diagnosticContextKind = "diagnostic_context"
const diagnosticModelPhase = "model"
const diagnosticsTimeout = time.Second

// startAgentDiagnostics is best effort only within its own time budget and for an
// invalid stored context. Caller cancellation and SQL failure reach the caller.
func (b *Bot) startAgentDiagnostics(
	ctx context.Context,
	owner string,
	updateID int64,
) (context.Context, *observability.AgentSpan, error) {
	if b.Logger == nil {
		return ctx, nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, diagnosticsTimeout)
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
		if ctx.Err() != nil {
			return ctx, nil, ctx.Err()
		}
		if callCtx.Err() != nil {
			b.Logger.WarnContext(ctx, "agent diagnostics omitted")
			return ctx, nil, nil
		}
		return ctx, nil, core.DatabaseOperationError(err)
	}
	recorder, err := observability.NewAgentEvents(b.Logger, correlation, attempt)
	if err != nil {
		b.Logger.WarnContext(ctx, "agent diagnostics omitted")
		return ctx, nil, nil
	}
	ctx, span := observability.StartAgentEvent(observability.WithAgentEvents(ctx, recorder),
		observability.AgentEvent{Phase: "request", Operation: "telegram.update"})
	return ctx, span, nil
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
