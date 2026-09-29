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
	scriptMassageBook      = "massage.book"
	scriptMassageCancel    = "massage.cancel"
	scriptMassageInstant   = "massage.practitioner.instant"
	scriptMassageConfigure = "massage.practitioner.configure"
)

func (b *Bot) scriptMassageEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	capabilities, err := b.API.PrivilegedReadCapabilities(ctx, owner)
	if err != nil {
		return nil, err
	}
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptMassageBook,
			Description: "Book your explicitly requested massage. Use party/provider IDs and absolute start from massage.slots; length uses the same units as slots. Host resolves the slot, rechecks availability and owns identity/replay. Displays refreshed booking controls.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"party":{"type":"string","maxLength":200},"specialist":{"type":"string","maxLength":200},"start":{"type":"string","format":"date-time"},"length":{"enum":[1,2,3,5]}},"required":["event","party","specialist","start","length"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptMassageCancel,
			Description: "Cancel your explicitly selected booking from massage.bookings. Host reads its current version and verifies ownership; cannot cancel another person's appointment. Displays refreshed booking controls.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"booking":{"type":"string","maxLength":200}},"required":["event","booking"],"additionalProperties":false}`,
			),
		},
	}
	if capabilities.PractitionerReads {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptMassageInstant,
				Description: "Reserve your own current practitioner time. Event and current party must be explicit; host chooses the current slot and your identity. Only a current practitioner of that event may use this. Length is one to six massage units.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"party":{"type":"string","maxLength":200},"length":{"type":"integer","minimum":1,"maximum":6}},"required":["event","party","length"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptMassageConfigure,
				Description: "Set both of your practitioner notification preferences as explicitly requested for an event. bookings controls new/cancelled booking notices; next controls next-client notices. Current membership is required, other practitioners are inaccessible.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"bookings":{"type":"boolean"},"next":{"type":"boolean"}},"required":["event","bookings","next"],"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]agenthost.ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(entries, agenthost.ScriptToolEntry{Descriptor: descriptor, Prepare: b.prepareMassageTool,
			Execute: b.executeMassageTool, ResultLimit: maxScriptReadBytes})
	}
	return entries, nil
}

func (b *Bot) prepareMassageTool(ctx context.Context, owner string, update int64,
	call scriptclient.ToolCall, _ agent.Input) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return record, errors.New("tool unavailable")
	}
	request, err := b.bindMassageTool(ctx, owner, call)
	if err != nil {
		return record, err
	}
	request.Update = update
	record.Massage = &request
	return record, nil
}

func (b *Bot) executeMassageTool(ctx context.Context, owner string, _ scriptclient.ToolCall,
	record agenthost.ScriptToolRecord, _ *agent.Input) (any, error) {
	request := record.Massage
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner {
		return nil, errors.New("tool unavailable")
	}
	if record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("missing admitted source")
	}
	var result any
	var err error
	switch {
	case request.Command != nil:
		result, err = b.Host.ExecuteDerivedMassage(ctx, owner, *request.Command, *record.Source)
	case request.Preferences != nil:
		result, err = b.Host.SetDerivedMassagePreferences(
			ctx,
			owner,
			request.Event,
			*request.Preferences,
			*record.Source,
		)
	default:
		return nil, errors.New("tool unavailable")
	}
	if err != nil {
		return nil, err
	}
	state := massageView{Event: request.Event, View: massageMine}
	if err = b.saveMassageState(ctx, owner, source.in.chat, request.Update, state); err != nil {
		return nil, err
	}
	if err = b.RenderMassage(ctx, owner, source.in.chat, ""); err != nil {
		return nil, err
	}
	return result, nil
}
