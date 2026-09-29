package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const (
	scriptDomainPageItems     = 20
	massagePartyQuery         = "party"
	scriptPassEvents          = "passes.events"
	scriptPassGet             = "passes.get"
	scriptPassInvitations     = "passes.invitations"
	scriptPassEventRead       = "passes.event.read"
	scriptMassageParties      = "massage.parties"
	scriptMassageSlots        = "massage.slots"
	scriptMassageBookings     = "massage.bookings"
	scriptMassageProviderRead = "massage.provider.read"
	scriptHistoryPageToolName = "history.page"
	scriptOrdersPageToolName  = "orders.page"
)

type scriptDomainArguments struct {
	Provider string `json:"provider"`
	Event    string `json:"event"`
	Party    string `json:"party"`
	Length   int    `json:"length"`
	Cursor   string `json:"cursor"`
}

type scriptDomainBookingArguments struct {
	Event  string `json:"event,omitempty"`
	Party  string `json:"party,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type scriptDomainSlotsArguments struct {
	scriptDomainBookingArguments

	Length int `json:"length"`
}

// A changed source page invalidates an offset rather than silently skipping data.
// RemoteNext is an existing Core keyset cursor, used only after this page ends.
func scriptDomainItems[T any](
	items []T,
	cursor scriptReadCursor,
	remoteNext string,
) (agenthost.ScriptDomainPage[T], error) {
	result := agenthost.ScriptDomainPage[T]{Items: []T{}}
	raw, err := json.Marshal(items)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(digest[:])
	if cursor.Digest != "" && cursor.Digest != fingerprint {
		return result, appclient.ErrReadStale
	}
	if cursor.Offset > len(items) {
		return result, errors.New("invalid read cursor")
	}
	for index := cursor.Offset; index < len(items); index++ {
		previous := result
		result.Items = append(result.Items, items[index])
		result.More = index+1 < len(items) || remoteNext != ""
		result.NextCursor = scriptDomainNextCursor(cursor, index+1, len(items), fingerprint, remoteNext)
		encoded, encodeErr := json.Marshal(agenthost.ModelToolEvidence(result))
		if encodeErr != nil {
			return result, encodeErr
		}
		if len(encoded) > maxScriptReadBytes {
			if len(previous.Items) == 0 {
				return previous, appclient.ErrReadLimit
			}
			return previous, nil
		}
		if len(result.Items) == scriptDomainPageItems {
			return result, nil
		}
	}
	return result, nil
}

func scriptDomainNextCursor(cursor scriptReadCursor, offset, count int, digest, remoteNext string) string {
	if offset == count && remoteNext == "" {
		return ""
	}
	cursor.Offset, cursor.Digest = offset, digest
	if offset == count {
		cursor.Position, cursor.Offset, cursor.Digest = remoteNext, 0, ""
	}
	return encodeScriptCursor(cursor)
}
func prepareScriptDomainRead(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var target any
	switch call.Name {
	case scriptPassEvents:
		target = new(scriptReadArguments)
	case scriptPassGet:
		target = new(struct {
			Event string `json:"event"`
		})
	case scriptPassInvitations, scriptPassEventRead, scriptMassageParties:
		target = new(agenthost.ScriptDomainEventArguments)
	case scriptMassageBookings:
		target = new(scriptDomainBookingArguments)
	case scriptMassageSlots:
		target = new(scriptDomainSlotsArguments)
	case scriptMassageProviderRead:
		target = new(struct {
			Event    string `json:"event,omitempty"`
			Provider string `json:"provider"`
			Cursor   string `json:"cursor,omitempty"`
		})
	default:
		return record, errors.New("tool unavailable")
	}
	if err := decodeScriptArguments(call.Arguments, target); err != nil {
		return record, err
	}
	var args scriptDomainArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	const maxDomainID = 200
	if len(args.Event) > maxDomainID || len(args.Party) > maxDomainID || len(args.Provider) > maxDomainID {
		return record, errors.New("invalid domain reference")
	}
	if (call.Name == scriptPassGet || call.Name == scriptPassInvitations || call.Name == scriptPassEventRead) &&
		args.Event == "" {
		return record, errors.New("event is required")
	}
	if call.Name == scriptMassageSlots && (args.Party == "" || args.Length <= 0) {
		return record, errors.New("party and length are required")
	}
	if call.Name == scriptMassageProviderRead && args.Provider == "" {
		return record, errors.New("provider is required")
	}
	if call.Name == scriptPassInvitations {
		record.PassRead = &agenthost.ScriptDomainEventArguments{Event: args.Event, Cursor: args.Cursor}
	}
	return record, nil
}

func scriptDomainScope(args scriptDomainArguments) string {
	args.Cursor = ""
	encoded, _ := json.Marshal(args)
	return string(encoded)
}

func (b *Bot) executeScriptDomainRead(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	_ agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	var args scriptDomainArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	switch call.Name {
	case scriptMassageParties, scriptMassageBookings, scriptMassageSlots, scriptMassageProviderRead:
		if args.Event == "" {
			args.Event = b.currentOrderEvent()
		}
	}
	switch call.Name {
	case scriptPassEvents:
		return b.API.PassEventsPage(ctx, owner, args.Cursor)
	case scriptPassEventRead:
		return b.API.PassEventDetail(ctx, owner, args.Event, args.Cursor)
	case scriptMassageSlots:
		return b.API.MassageSlotsPage(ctx, owner, args.Event, args.Party, args.Cursor, args.Length)
	case scriptMassageProviderRead:
		return b.API.MassageProviderDetail(ctx, owner, args.Event, args.Provider, args.Cursor)
	}
	cursor, err := readScriptCursor(args.Cursor, owner, call.Name, scriptDomainScope(args))
	if err != nil {
		return nil, err
	}
	switch call.Name {
	case scriptPassGet:
		return b.API.PassBooking(ctx, owner, args.Event)
	case scriptPassInvitations:
		page, readErr := b.API.PassInvitations(ctx, owner, args.Event, cursor.Position)
		if readErr != nil {
			return nil, readErr
		}
		return scriptDomainItems(page.Invitations, cursor, page.Next)
	case scriptMassageParties:
		items, readErr := b.API.MassageParties(ctx, owner, args.Event)
		if readErr != nil {
			return nil, readErr
		}
		return scriptDomainItems(items, cursor, "")
	case scriptMassageBookings:
		items, readErr := b.API.MassageBookings(ctx, owner, args.Event, args.Party, "mine")
		if readErr != nil {
			return nil, readErr
		}
		return scriptDomainItems(items, cursor, "")
	default:
		return nil, errors.New("tool unavailable")
	}
}

func (b *Bot) scriptDomainEntries() []agenthost.ScriptToolEntry {
	cursor := json.RawMessage(
		`{"type":"object","properties":{"cursor":{"type":"string"}},"additionalProperties":false}`,
	)
	event := json.RawMessage(
		`{"type":"object","properties":{"event":{"type":"string"}},"required":["event"],"additionalProperties":false}`,
	)
	eventPage := json.RawMessage(
		`{"type":"object","properties":{"event":{"type":"string"},"cursor":{"type":"string"}},"required":["event"],"additionalProperties":false}`,
	)
	massagePage := json.RawMessage(
		`{"type":"object","properties":{"event":{"type":"string"},"cursor":{"type":"string"}},"additionalProperties":false}`,
	)
	bookings := json.RawMessage(
		`{"type":"object","properties":{"event":{"type":"string"},"party":{"type":"string"},"cursor":{"type":"string"}},"additionalProperties":false}`,
	)
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptPassEvents,
			Description: "Browse active pass events and your own historical registrations with excerpted en/ru titles and stable IDs. Historical events are read-only; use passes.registration.read/show with home/payment views. Follow next_cursor; passes.event.read returns complete localized titles.",
			InputSchema: cursor,
		},
		{
			Name:        scriptPassGet,
			Description: "Read your current pass booking for the selected event. Does not authorize any booking mutation.",
			InputSchema: event,
		},
		{
			Name:        scriptPassInvitations,
			Description: "Read invitations addressed to you for an event. Follow next_cursor; stale means restart.",
			InputSchema: eventPage,
		},
		{
			Name:        scriptMassageParties,
			Description: "Browse massage parties. Event defaults to the current event. Follow next_cursor; stale means restart.",
			InputSchema: massagePage,
		},
		{
			Name:        scriptMassageBookings,
			Description: "Read only your massage bookings, optionally filtered by party. Event defaults to current event. Follow next_cursor; stale means restart.",
			InputSchema: bookings,
		},
	}
	descriptors = append(
		descriptors,
		scriptclient.Tool{
			Name:        scriptPassEventRead,
			Description: "Read complete event JSON, including every localized title, in Unicode-safe json chunks. Join chunks then JSON.parse. Follow next_cursor; stale means discard chunks and restart. Resource cap1MiB serialized JSON; result_limit means unavailable at this size.",
			InputSchema: eventPage,
		},
		scriptclient.Tool{
			Name:        scriptMassageSlots,
			Description: "Browse eligible appointment slots with explicitly excerpted provider names/icons and stable provider IDs. Event defaults to current. Follow next_cursor; stale means restart. Use massage.provider.read for complete public provider details.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event":{"type":"string"},"party":{"type":"string"},"length":{"type":"integer"},"cursor":{"type":"string"}},"required":["party","length"],"additionalProperties":false}`,
			),
		},
		scriptclient.Tool{
			Name:        scriptMassageProviderRead,
			Description: "Read complete public provider JSON as Unicode-safe json chunks. Join then JSON.parse. Event defaults to current; follow next_cursor. Stale means discard chunks and restart; serialized resource cap1MiB, otherwise result_limit. No private scheduling or notification fields.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event":{"type":"string"},"provider":{"type":"string"},"cursor":{"type":"string"}},"required":["provider"],"additionalProperties":false}`,
			),
		},
	)
	entries := make([]agenthost.ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			agenthost.ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     prepareScriptDomainRead,
				Execute:     b.executeScriptDomainRead,
				ResultLimit: maxScriptReadBytes,
			},
		)
	}
	return entries
}
