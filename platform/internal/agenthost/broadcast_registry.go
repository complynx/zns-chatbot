package agenthost

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptBroadcastPreview = "broadcasts.preview"
const scriptBroadcastPending = "broadcasts.pending"
const scriptBroadcastAttach = "broadcasts.attach"
const scriptBroadcastCancel = "broadcasts.cancel"
const scriptBroadcastReview = "broadcasts.review"
const scriptBroadcastShow = "broadcasts.show"
const scriptBroadcastAudience = "broadcasts.audience"
const scriptBroadcastProfile = "broadcasts.profile"

// BroadcastScriptCatalog owns live visibility and schemas for preview and review.
// Neither binding provides model-controlled send or confirmation operations.
type BroadcastScriptCatalog struct {
	Allowed       func(context.Context, string) (bool, error)
	ActionBinding ScriptToolEntry
	ReviewBinding ScriptToolEntry
	ReadBinding   ScriptToolEntry
}

func (c BroadcastScriptCatalog) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	allowed, err := c.Allowed(ctx, owner)
	if err != nil || !allowed {
		return nil, err
	}
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	id := json.RawMessage(
		`{"type":"object","properties":{"input_id":{"type":"integer","minimum":1}},"required":["input_id"],"additionalProperties":false}`,
	)
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptBroadcastPreview,
			Description: "Create an administrator broadcast preview from an explicit /send_message_to command. Recipients must reflect the user's request. Missing content asks for input. Sending always requires a manual confirmation button.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"command":{"type":"string","maxLength":16384}},"required":["command"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptBroadcastPending,
			Description: "Read pending broadcast input hints in this chat. They never make unrelated messages broadcast content.",
			InputSchema: empty,
		},
		{
			Name:        scriptBroadcastAttach,
			Description: "Use the current received message as content for a pending input only when the user requests that intent. Source chat/message/content are host-bound. Creates preview, never sends.",
			InputSchema: id,
		},
		{Name: scriptBroadcastCancel, Description: "Cancel an owner-scoped pending broadcast input.", InputSchema: id},
	}
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     c.ActionBinding.Prepare,
				Execute:     c.ActionBinding.Execute,
				ResultLimit: c.ActionBinding.ResultLimit,
			},
		)
	}
	entries = append(entries, c.readEntries()...)
	return append(entries, c.reviewEntries()...), nil
}

func (c BroadcastScriptCatalog) reviewEntries() []ScriptToolEntry {
	tools := []scriptclient.Tool{
		{
			Name:        scriptBroadcastReview,
			Description: "Read your broadcast campaign status and complete recipient content/results without changing it. Each offset selects a page of up to 20 recipients, serialized into JSON chunks. Concatenate json until more=false and JSON.parse; then advance offset by page.items.length while page.more. Reuse id/offset with next_cursor for chunks. A stale read requires restarting that page. Delivery attempts are not proof of exactly-once delivery.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"id":{"type":"integer","minimum":1},"offset":{"type":"integer","minimum":0},"cursor":{"type":"string","maxLength":2048}},"required":["id"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptBroadcastShow,
			Description: "Show your campaign's existing native review and manual continuation buttons in the current chat. May resume unfinished content preparation through the existing renderer, but never enqueues or sends the campaign. Sending requires the user's separate manual Send confirmation. Interrupted display may have reached Telegram; inspect host outcomes before requesting another display.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"id":{"type":"integer","minimum":1},"offset":{"type":"integer","minimum":0}},"required":["id"],"additionalProperties":false}`,
			),
		},
	}
	entries := make([]ScriptToolEntry, 0, len(tools))
	for _, tool := range tools {
		entries = append(entries, ScriptToolEntry{
			Descriptor: tool, Prepare: c.ReviewBinding.Prepare, Execute: c.ReviewBinding.Execute,
			ResultLimit: c.ReviewBinding.ResultLimit,
		})
	}
	return entries
}

func (c BroadcastScriptCatalog) readEntries() []ScriptToolEntry {
	tools := []scriptclient.Tool{
		{
			Name:        scriptBroadcastAudience,
			Description: "Administrator-only keyset pages of same-deployment user IDs, display-name excerpts and available profile field names. Follow next_cursor until more=false. Use broadcasts.profile to inspect complete values and filter with JavaScript; preview explicit recipient IDs afterward. No SQL.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"cursor":{"type":"string","maxLength":2048}},"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptBroadcastProfile,
			Description: "Read an authorized broadcast profile as complete JSON chunks. Concatenate json fields until more=false, then JSON.parse. Missing fields stay absent, null stays null. Stale means restart; do not use incomplete chunks. Current global administrator access required.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"user_id":{"type":"string","maxLength":20},"cursor":{"type":"string","maxLength":2048}},"required":["user_id"],"additionalProperties":false}`,
			),
		},
	}
	entries := make([]ScriptToolEntry, 0, len(tools))
	for _, tool := range tools {
		entries = append(
			entries,
			ScriptToolEntry{
				Descriptor:  tool,
				Prepare:     c.ReadBinding.Prepare,
				Execute:     c.ReadBinding.Execute,
				ResultLimit: c.ReadBinding.ResultLimit,
			},
		)
	}
	return entries
}
