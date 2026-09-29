package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const (
	scriptPrivilegeEvents         = "privileges.events"
	scriptPaymentQueue            = "passes.payments.queue"
	scriptPaymentHistory          = "passes.payments.history"
	scriptPractitionerSchedule    = "massage.practitioner.schedule"
	scriptPractitionerPreferences = "massage.practitioner.preferences"
	scriptPractitionerBookings    = "massage.practitioner.bookings"
)

type scriptPrivilegedArguments struct {
	Event  string `json:"event"`
	Party  string `json:"party,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// Scope is bound before admission; it cannot be reconstructed from transformed
// script output. Admission retains an exact role for empty event-list pages.

func (b *Bot) scriptPrivilegedReadEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	capabilities, err := b.API.PrivilegedReadCapabilities(ctx, owner)
	if err != nil {
		return nil, err
	}
	if !capabilities.PaymentReads && !capabilities.PractitionerReads {
		return nil, nil
	}
	eventPage := json.RawMessage(
		`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"cursor":{"type":"string","maxLength":2048}},"required":["event"],"additionalProperties":false}`,
	)
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptPrivilegeEvents,
			Description: "Browse event IDs where you currently have a privileged read role, including historical events. Flags apply only to that event. Follow next_cursor while more is true; each read checks current access.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"cursor":{"type":"string","maxLength":2048}},"additionalProperties":false}`,
			),
		},
	}
	if capabilities.PaymentReads {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptPaymentQueue,
				Description: "Read pending payment reviews for an event where you are currently a payment administrator. Follow next_cursor; stale means restart. Does not download proofs or change decisions.",
				InputSchema: eventPage,
			},
			scriptclient.Tool{
				Name:        scriptPaymentHistory,
				Description: "Read historical payment attempts and participant snapshots for an event where you are currently a payment administrator. Includes replaced attempts; each item is one attempt-participant snapshot, not a current booking. Follow next_cursor. Unknown actors remain unknown; no raw proofs.",
				InputSchema: eventPage,
			},
		)
	}
	if capabilities.PractitionerReads {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptPractitionerSchedule,
				Description: "Read only your own practitioner work spans for the explicit event, in start/ID order. Follow next_cursor. Current practitioner membership is required on each page.",
				InputSchema: eventPage,
			},
			scriptclient.Tool{
				Name:        scriptPractitionerPreferences,
				Description: "Read your own practitioner notification preferences for the explicit event. Current practitioner membership is required. This does not change preferences.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200}},"required":["event"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptPractitionerBookings,
				Description: "Read active bookings assigned to you as practitioner, optionally filtered by party. Excludes cancelled and other practitioners' bookings. Follow next_cursor; current membership is required on each page.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"party":{"type":"string","maxLength":200},"cursor":{"type":"string","maxLength":2048}},"required":["event"],"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]agenthost.ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			agenthost.ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     b.prepareScriptPrivilegedRead,
				Execute:     b.executeScriptPrivilegedRead,
				ResultLimit: maxScriptReadBytes,
			},
		)
	}
	return entries, nil
}

func prepareScriptPrivilegedRead(
	_ context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var target any
	switch call.Name {
	case scriptPrivilegeEvents:
		target = new(scriptReadArguments)
	case scriptPractitionerPreferences:
		target = new(struct {
			Event string `json:"event"`
		})
	case scriptPractitionerBookings:
		target = new(scriptPrivilegedArguments)
	case scriptPaymentQueue, scriptPaymentHistory, scriptPractitionerSchedule:
		target = new(agenthost.ScriptDomainEventArguments)
	default:
		return record, errors.New("tool unavailable")
	}
	if err := decodeScriptArguments(call.Arguments, target); err != nil {
		return record, err
	}
	var args scriptPrivilegedArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	const maxPrivilegedID = 200
	const maxPrivilegedCursor = 2048
	if len(args.Event) > maxPrivilegedID || len(args.Party) > maxPrivilegedID ||
		len(args.Cursor) > maxPrivilegedCursor ||
		(call.Name != scriptPrivilegeEvents && args.Event == "") {
		return record, errors.New("invalid privileged read arguments")
	}
	record.PrivilegedRead = &agenthost.ScriptPrivilegedRead{Event: args.Event, Owner: owner}
	return record, nil
}

func (b *Bot) prepareScriptPrivilegedRead(
	ctx context.Context,
	owner string,
	update int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record, err := prepareScriptPrivilegedRead(ctx, owner, update, call, input)
	if err != nil || call.Name != scriptPrivilegeEvents {
		return record, err
	}
	page, err := b.API.PrivilegedReadEvents(ctx, owner, "")
	if err != nil {
		return record, err
	}
	if len(page.Items) == 0 {
		return record, errors.New("privileged read admission missing")
	}
	record.PrivilegedRead.Admission = agenthost.PrivilegedEventAuthorities(owner, page.Items[:1])
	if len(record.PrivilegedRead.Admission) == 0 {
		return record, errors.New("privileged read admission missing")
	}
	return record, nil
}

func (b *Bot) executeScriptPrivilegedRead(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	var args scriptPrivilegedArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	if record.PrivilegedRead == nil || record.PrivilegedRead.Owner != owner ||
		record.PrivilegedRead.Event != args.Event {
		return nil, errors.New("privileged read binding missing")
	}
	switch call.Name {
	case scriptPrivilegeEvents:
		return b.API.PrivilegedReadEvents(ctx, owner, args.Cursor)
	case scriptPaymentHistory:
		return b.API.PassPaymentHistory(ctx, owner, args.Event, args.Cursor)
	case scriptPractitionerPreferences:
		return b.API.MassagePreferences(ctx, owner, args.Event)
	case scriptPractitionerSchedule:
		return b.API.PractitionerSchedule(ctx, owner, args.Event, args.Cursor)
	case scriptPractitionerBookings:
		return b.API.PractitionerBookings(ctx, owner, args.Event, args.Party, args.Cursor)
	case scriptPaymentQueue:
		cursor, err := readScriptCursor(args.Cursor, owner, call.Name, args.Event)
		if err != nil {
			return nil, err
		}
		page, err := b.API.PassPaymentQueue(ctx, owner, args.Event, cursor.Position)
		if err != nil {
			return nil, err
		}
		return scriptDomainItems(page.Items, cursor, page.Next)
	default:
		return nil, errors.New("tool unavailable")
	}
}
