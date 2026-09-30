package agenthost

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const (
	scriptMassageBook      = "massage.book"
	scriptMassageCancel    = "massage.cancel"
	scriptMassageInstant   = "massage.practitioner.instant"
	scriptMassageConfigure = "massage.practitioner.configure"
)

// MassageCapabilityReader retains the current domain role projection. It does
// not authorize a selected practitioner event or another person's booking.
type MassageCapabilityReader interface {
	PrivilegedReadCapabilities(context.Context, string) (core.PrivilegedReadCapabilities, error)
}

// MassageScriptCatalog owns live visibility and schemas for massage actions.
// Binding supplies existing domain callbacks and the original result bound.
type MassageScriptCatalog struct {
	Client  MassageCapabilityReader
	Binding ScriptToolEntry
}

func (c MassageScriptCatalog) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	capabilities, err := c.Client.PrivilegedReadCapabilities(ctx, owner)
	if err != nil {
		return nil, err
	}
	descriptors := []scriptclient.Tool{
		{
			Name: scriptMassageBook,
			Description: "Book your explicitly requested massage. Use party/provider IDs and absolute start " +
				"from massage.slots; length uses the same units as slots. Host resolves the slot, " +
				"rechecks availability and owns identity/replay. Displays refreshed booking controls.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"party":{"type":"string",` +
					`"maxLength":200},"specialist":{"type":"string","maxLength":200},"start":{"type":"string",` +
					`"format":"date-time"},"length":{"enum":[1,2,3,5]}},"required":["event","party","specialist","start",` +
					`"length"],"additionalProperties":false}`,
			),
		},
		{
			Name: scriptMassageCancel,
			Description: "Cancel your explicitly selected booking from massage.bookings. Host reads its current " +
				"version and verifies ownership; cannot cancel another person's appointment. Displays " +
				"refreshed booking controls.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"booking":{"type":"string",` +
					`"maxLength":200}},"required":["event","booking"],"additionalProperties":false}`,
			),
		},
	}
	if capabilities.PractitionerReads {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name: scriptMassageInstant,
				Description: "Reserve your own current practitioner time. Event and current party must be explicit; " +
					"host chooses the current slot and your identity. Only a current practitioner of that " +
					"event may use this. Length is one to six massage units.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"party":{"type":"string",` +
						`"maxLength":200},"length":{"type":"integer","minimum":1,"maximum":6}},"required":["event","party",` +
						`"length"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name: scriptMassageConfigure,
				Description: "Set both of your practitioner notification preferences as explicitly requested for an " +
					"event. bookings controls new/cancelled booking notices; next controls next-client " +
					"notices. Current membership is required, other practitioners are inaccessible.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200},` +
						`"bookings":{"type":"boolean"},"next":{"type":"boolean"}},"required":["event","bookings","next"],` +
						`"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(entries, ScriptToolEntry{Descriptor: descriptor, Prepare: c.Binding.Prepare,
			Execute: c.Binding.Execute, ResultLimit: c.Binding.ResultLimit})
	}
	return entries, nil
}
