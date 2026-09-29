package agenthost

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const (
	hostAdminAssign                    = "admin_assign"
	hostAdminCancel                    = "admin_cancel"
	hostHistoryPage                    = "history.page"
	hostHistoryRead                    = "history.read"
	hostMassagePractitionerBookings    = "massage.practitioner.bookings"
	hostMassagePractitionerPreferences = "massage.practitioner.preferences"
	hostMassagePractitionerSchedule    = "massage.practitioner.schedule"
	hostPassesAdminAssign              = "passes.admin.assign"
	hostPassesAdminQueue               = "passes.admin.queue"
	hostPassesAdminTarget              = "passes.admin.target"
	hostPassesEvents                   = "passes.events"
	hostPassesExport                   = "passes.export"
	hostPassesInvitations              = "passes.invitations"
	hostPassesPaymentsHistory          = "passes.payments.history"
	hostPassesPaymentsQueue            = "passes.payments.queue"
	hostPassesPaymentsReview           = "passes.payments.review"
	hostPassesTakeoverRead             = "passes.takeover.read"
	hostPassesTiers                    = "passes.tiers"
	hostPaymentQueue                   = "payment_queue"
	hostPrivilegesEvents               = "privileges.events"
	hostProofAccept                    = "proof_accept"
	hostQueue                          = "queue"
)

func scriptInputAuthorities(input *agent.Input) ([]readsource.Authority, error) {
	refs, err := HistoryInputReadAuthorities(input)
	if err != nil {
		return nil, err
	}
	return MergeReadAuthorities(refs, KnowledgeReadAuthorities(input.Knowledge))
}

// ScriptCallProjection keeps intermediate reads in owner-private receipts; the script chooses the
// bounded evidence to return to the next model prompt.
func ScriptCallProjection(outcome agent.ScriptToolResult) agent.ScriptToolResult {
	switch outcome.Name {
	case "lineup.query", "knowledge.read",
		"knowledge.proposals",
		"knowledge.review_queue",
		"knowledge.memos",
		"knowledge." + agent.KnowledgeMemoRead,
		"passes.registration.read",
		hostPassesAdminQueue,
		hostPassesAdminTarget,
		hostPassesPaymentsReview,
		hostPassesTakeoverRead,
		hostPassesTiers,
		hostHistoryPage,
		hostHistoryRead,
		"orders.events", "orders.event", "orders.browse", "orders.contacts", "orders.history.page", "orders.history.read",
		"orders.choice", "orders.inspect", "orders.instructions", "orders.quote", "orders.inbox", "orders.review.read",
		"food.view",
		"orders.page",
		"orders.read",
		hostPassesEvents,
		"passes.get",
		hostPassesInvitations,
		"passes.event.read",
		"massage.parties",
		"massage.slots",
		"massage.bookings",
		"massage.provider.read",
		"broadcasts.audience", "broadcasts.profile", "broadcasts.review",
		hostPrivilegesEvents, hostPassesPaymentsQueue, hostPassesPaymentsHistory, hostMassagePractitionerSchedule, hostMassagePractitionerPreferences, hostMassagePractitionerBookings:
		if outcome.Error == "" {
			outcome.Result = json.RawMessage(
				`{"read_completed":true,"payload_omitted":true,"evidence":"See script result; omission is not absence."}`,
			)
		}
		return outcome
	default:
		return memoryCallProjection(outcome)
	}
}

func memoryCallProjection(outcome agent.ScriptToolResult) agent.ScriptToolResult {
	if strings.HasPrefix(outcome.Name, "memory.") && outcome.Name != "memory.write" && outcome.Error == "" {
		outcome.Result = json.RawMessage(
			`{"read_completed":true,"payload_omitted":true,"evidence":"See script result; omission is not absence."}`,
		)
	}
	return outcome
}

// ScriptToolResultMetadata counts known page fields without response content.
func ScriptToolResultMetadata(name string, data json.RawMessage) (int, bool) {
	if string(data) == "[]" {
		return 0, true
	}
	var field string
	switch name {
	case hostPassesEvents, hostPassesInvitations, "massage.parties", "massage.slots", "massage.bookings",
		hostPrivilegesEvents, hostPassesPaymentsQueue, hostPassesPaymentsHistory, hostMassagePractitionerSchedule, hostMassagePractitionerBookings:
		field = "items"
	case hostHistoryPage:
		field = "events"
	case "orders.page":
		field = "orders"
	default:
		return 0, false
	}
	var page struct {
		Items  []json.RawMessage `json:"items"`
		Events []json.RawMessage `json:"events"`
		Orders []json.RawMessage `json:"orders"`
		More   bool              `json:"more"`
		Error  string            `json:"error"`
	}
	if json.Unmarshal(data, &page) != nil || page.Error != "" {
		return 0, false
	}
	var items []json.RawMessage
	switch field {
	case "items":
		items = page.Items
	case "events":
		items = page.Events
	case "orders":
		items = page.Orders
	}
	if items == nil {
		return 0, false
	}
	return len(items), len(items) == 0 && !page.More
}

func ScriptSourceFailure(err, staleError error) json.RawMessage {
	if staleError != nil && errors.Is(err, staleError) {
		return json.RawMessage(`{"error":"stale","restart":true}`)
	}
	if errors.Is(err, readsource.ErrLimit) {
		return json.RawMessage(`{"error":"source_authority_limit","new_turn_required":true}`)
	}
	var problem *core.ProblemError
	if errors.As(err, &problem) && problem.Code == sourceAuthorityLimit {
		return json.RawMessage(`{"error":"source_authority_limit","new_turn_required":true}`)
	}
	return nil
}

func NormalizeScriptOutcome(
	result any,
	err error,
	diagnostic *observability.AgentSpan,
	staleError, readLimitError error,
) (any, string, error) {
	outcomeError := ""
	if response := ScriptSourceFailure(err, staleError); response != nil {
		result = response
		outcomeError = sourceAuthorityLimit
		if errors.Is(err, staleError) {
			// A cursor conflict is recoverable inside this run. Only admission
			// proves causal source loss and stops the worker child context.
			outcomeError = "stale"
			diagnostic.Outcome("error", "conflict")
		} else {
			diagnostic.Outcome("limited", outcomeError)
		}
		err = nil
	}
	if readLimitError != nil && errors.Is(err, readLimitError) {
		result = json.RawMessage(`{"error":"result_limit"}`)
		err = nil
		outcomeError = "result_limit"
		diagnostic.Outcome("limited", "result_limit")
	}
	return result, outcomeError, err
}
