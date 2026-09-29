package observability

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const agentEventVersion = 1
const maxAgentEvents = 256
const diagnosticUnknown = "unknown"
const maxDiagnosticBytes = 1 << 30
const maxDiagnosticResults = 1 << 20
const maxDiagnosticDuration = 24 * time.Hour

// AgentEvent contains metadata only. Never put user text or tool values in it.
type AgentEvent struct {
	Phase       string        `json:"phase"`
	Operation   string        `json:"operation"`
	Outcome     string        `json:"outcome"`
	ErrorCode   string        `json:"error_code,omitempty"`
	Parent      uint64        `json:"parent,omitempty"`
	Replay      bool          `json:"replay,omitempty"`
	Elapsed     time.Duration `json:"-"`
	InputBytes  int           `json:"input_bytes,omitempty"`
	OutputBytes int           `json:"output_bytes,omitempty"`
	ResultCount int           `json:"result_count,omitempty"`
	Incomplete  bool          `json:"incomplete,omitempty"`
}

// AgentRecord is the versioned diagnostic wire format, not a domain receipt.
type AgentRecord struct {
	AgentEvent

	Version     int          `json:"version"`
	At          time.Time    `json:"at"`
	Correlation string       `json:"correlation"`
	Attempt     uint32       `json:"attempt"`
	Sequence    uint64       `json:"sequence"`
	ElapsedMS   int64        `json:"elapsed_ms,omitempty"`
	Code        *CodeProfile `json:"code,omitempty"`
}

// AgentEvents serializes events within one attempt. The host persists correlation
// across retries and supplies a new attempt number, never a user-selected ID.
type AgentEvents struct {
	mu          sync.Mutex
	logger      *slog.Logger
	correlation string
	attempt     uint32
	sequence    uint64
}

// NewAgentEvents accepts an opaque UUID and a positive host attempt number.
func NewAgentEvents(logger *slog.Logger, correlation string, attempt uint32) (*AgentEvents, error) {
	id, err := uuid.Parse(correlation)
	if err != nil || id == uuid.Nil || id.String() != correlation || attempt == 0 || logger == nil {
		return nil, errors.New("invalid diagnostic context")
	}
	return &AgentEvents{logger: logger, correlation: correlation, attempt: attempt}, nil
}

// Emit returns the sequence for child events. Zero means the attempt log is full.
// Event strings are finite allowlists; unknown values never reach the log.
func (r *AgentEvents) Emit(ctx context.Context, event AgentEvent) uint64 {
	return r.emit(ctx, event, nil)
}

// EmitCode records redacted code evidence, never original source or memo contents.
func (r *AgentEvents) EmitCode(ctx context.Context, event AgentEvent, code string) uint64 {
	profile := ProfileAgentCode(code)
	return r.emit(ctx, event, &profile)
}

func (r *AgentEvents) emit(ctx context.Context, event AgentEvent, profile *CodeProfile) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sequence >= maxAgentEvents {
		return 0
	}
	r.sequence++
	event = safeAgentEvent(event)
	if event.Parent >= r.sequence {
		event.Parent = 0
	}
	if r.sequence == maxAgentEvents {
		event = AgentEvent{Phase: "request", Operation: "diagnostic", Outcome: "limited", Incomplete: true}
		profile = nil
	}
	record := AgentRecord{
		AgentEvent:  event,
		Version:     agentEventVersion,
		At:          time.Now().UTC(),
		Correlation: r.correlation,
		Attempt:     r.attempt,
		Sequence:    r.sequence,
		ElapsedMS:   event.Elapsed.Milliseconds(),
		Code:        profile,
	}
	data, err := json.Marshal(record)
	if len(data) > 3500 && record.Code != nil {
		record.Code.Source = ""
		record.Code.Status = codeStructural
		data, err = json.Marshal(record)
	}
	if err != nil {
		return 0
	}
	r.logger.InfoContext(ctx, "agent diagnostic", "agent_event", string(data))
	return r.sequence
}

func safeAgentEvent(event AgentEvent) AgentEvent {
	event.Phase = diagnosticChoice(event.Phase, "request", "model", "tool", "script", "search")
	event.Operation = safeAgentOperation(event.Operation)
	event.Outcome = diagnosticChoice(event.Outcome, "started", "ok", "error", "canceled", "timeout", "denied",
		"replayed", "interrupted", "limited", "no_results", "invalid")
	if event.ErrorCode != "" {
		event.ErrorCode = diagnosticChoice(
			event.ErrorCode, "invalid_input", "invalid_query", "invalid_cursor", "invalid_plan", "unavailable",
			"conflict", "result_limit", "budget", "timeout", "canceled", "transport", "execution_failed",
		)
	}
	event.InputBytes = max(0, min(event.InputBytes, maxDiagnosticBytes))
	event.OutputBytes = max(0, min(event.OutputBytes, maxDiagnosticBytes))
	event.ResultCount = max(0, min(event.ResultCount, maxDiagnosticResults))
	event.Elapsed = max(0, min(event.Elapsed, maxDiagnosticDuration))
	return event
}

func diagnosticChoice(value string, allowed ...string) string {
	for _, item := range allowed {
		if value == item {
			return item
		}
	}
	return diagnosticUnknown
}
