package agenthost

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// PrivilegedReadClient retains domain-owned live role and event authorization.
type PrivilegedReadClient interface {
	PrivilegedReadCapabilities(context.Context, string) (core.PrivilegedReadCapabilities, error)
	PrivilegedReadEvents(context.Context, string, string) (core.ReadPage[core.PrivilegedReadEvent], error)
}

// PrivilegedReadHost owns live tool visibility and event-list admission.
// Binding supplies transport parsing and domain execution callbacks.
type PrivilegedReadHost struct {
	Client  PrivilegedReadClient
	Binding ScriptToolEntry
}

func (h PrivilegedReadHost) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	capabilities, err := h.Client.PrivilegedReadCapabilities(ctx, owner)
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
			Name:        hostPrivilegesEvents,
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
				Name:        hostPassesPaymentsQueue,
				Description: "Read pending payment reviews for an event where you are currently a payment administrator. Follow next_cursor; stale means restart. Does not download proofs or change decisions.",
				InputSchema: eventPage,
			},
			scriptclient.Tool{
				Name:        hostPassesPaymentsHistory,
				Description: "Read historical payment attempts and participant snapshots for an event where you are currently a payment administrator. Includes replaced attempts; each item is one attempt-participant snapshot, not a current booking. Follow next_cursor. Unknown actors remain unknown; no raw proofs.",
				InputSchema: eventPage,
			},
		)
	}
	if capabilities.PractitionerReads {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        hostMassagePractitionerSchedule,
				Description: "Read only your own practitioner work spans for the explicit event, in start/ID order. Follow next_cursor. Current practitioner membership is required on each page.",
				InputSchema: eventPage,
			},
			scriptclient.Tool{
				Name:        hostMassagePractitionerPreferences,
				Description: "Read your own practitioner notification preferences for the explicit event. Current practitioner membership is required. This does not change preferences.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200}},"required":["event"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        hostMassagePractitionerBookings,
				Description: "Read active bookings assigned to you as practitioner, optionally filtered by party. Excludes cancelled and other practitioners' bookings. Follow next_cursor; current membership is required on each page.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"event":{"type":"string","maxLength":200},"party":{"type":"string","maxLength":200},"cursor":{"type":"string","maxLength":2048}},"required":["event"],"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     h.Binding.Prepare,
				Execute:     h.Binding.Execute,
				ResultLimit: h.Binding.ResultLimit,
			},
		)
	}
	return entries, nil
}

// AdmitEventList binds one current exact event-role witness before admission.
// The first page is an authority witness, not the requested result page.
func (h PrivilegedReadHost) AdmitEventList(
	ctx context.Context, owner string, record ScriptToolRecord,
) (ScriptToolRecord, error) {
	if record.Outcome.Name != hostPrivilegesEvents || record.PrivilegedRead == nil ||
		record.PrivilegedRead.Owner != owner || record.PrivilegedRead.Event != "" {
		return record, errors.New("privileged read admission missing")
	}
	page, err := h.Client.PrivilegedReadEvents(ctx, owner, "")
	if err != nil {
		return record, err
	}
	if len(page.Items) == 0 {
		return record, errors.New("privileged read admission missing")
	}
	admission := PrivilegedEventAuthorities(owner, page.Items[:1])
	if len(admission) == 0 {
		return record, errors.New("privileged read admission missing")
	}
	record.PrivilegedRead.Admission = admission
	return record, nil
}
