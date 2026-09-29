package observability

import (
	"context"
	"errors"
	"time"
)

type diagnosticContextKey struct{}
type diagnosticContext struct {
	recorder *AgentEvents
	parent   uint64
}

// WithAgentEvents attaches host-only metadata. It is never provider input.
func WithAgentEvents(ctx context.Context, recorder *AgentEvents) context.Context {
	return context.WithValue(ctx, diagnosticContextKey{}, diagnosticContext{recorder: recorder})
}

// AgentSpan records one operation. A nil span makes instrumentation optional.
type AgentSpan struct {
	ctx      context.Context
	recorder *AgentEvents
	event    AgentEvent
	started  time.Time
}

// StartAgentEvent returns a child context with the start event as parent.
func StartAgentEvent(ctx context.Context, event AgentEvent) (context.Context, *AgentSpan) {
	return startAgentEvent(ctx, event, "")
}

// StartAgentCode records sanitized code only, without arguments or results.
func StartAgentCode(ctx context.Context, event AgentEvent, code string) (context.Context, *AgentSpan) {
	return startAgentEvent(ctx, event, code)
}

func startAgentEvent(ctx context.Context, event AgentEvent, code string) (context.Context, *AgentSpan) {
	current, ok := ctx.Value(diagnosticContextKey{}).(diagnosticContext)
	if !ok || current.recorder == nil {
		return ctx, nil
	}
	event.Parent, event.Outcome = current.parent, "started"
	var sequence uint64
	if code != "" {
		sequence = current.recorder.EmitCode(ctx, event, code)
	} else {
		sequence = current.recorder.Emit(ctx, event)
	}
	child := context.WithValue(
		ctx,
		diagnosticContextKey{},
		diagnosticContext{recorder: current.recorder, parent: sequence},
	)
	event.Parent = sequence
	return child, &AgentSpan{ctx: ctx, recorder: current.recorder, event: event, started: time.Now()}
}

// EmitAgentEvent records an instantaneous event, such as a proven cache replay.
func EmitAgentEvent(ctx context.Context, event AgentEvent) {
	if current, ok := ctx.Value(diagnosticContextKey{}).(diagnosticContext); ok && current.recorder != nil {
		event.Parent = current.parent
		current.recorder.Emit(ctx, event)
	}
}

// Outcome marks a known host outcome; neither parameter may contain user data.
func (s *AgentSpan) Outcome(outcome, code string) {
	if s != nil {
		s.event.Outcome, s.event.ErrorCode = outcome, code
	}
}

// Result records bounded metadata, never result content.
func (s *AgentSpan) Result(bytes, count int, empty bool) {
	if s != nil {
		s.event.OutputBytes, s.event.ResultCount = bytes, count
		if empty {
			s.event.Outcome = "no_results"
		}
	}
}

// Finish emits a terminal event. Error messages are never serialized.
func (s *AgentSpan) Finish(err error) {
	if s == nil {
		return
	}
	if s.event.Outcome == "started" {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			s.event.Outcome, s.event.ErrorCode = "timeout", ""
		case errors.Is(err, context.Canceled):
			s.event.Outcome, s.event.ErrorCode = "canceled", ""
		case err != nil:
			s.event.Outcome = "error"
		default:
			s.event.Outcome = "ok"
		}
	}
	s.event.Elapsed = time.Since(s.started)
	s.recorder.Emit(s.ctx, s.event)
}
