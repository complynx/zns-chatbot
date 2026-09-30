package agenthost

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptFoodView = "food.view"
const scriptFoodQuote = "food.quote"
const scriptFoodChange = "food.change"
const scriptFoodPrepare = "food.payment.prepare"
const scriptFoodReviewQueue = "food.review.queue"
const scriptFoodReviewRead = "food.review.read"
const scriptFoodReviewDecide = "food.review.decide"
const scriptFoodReviewProof = "food.review.proof"
const scriptFoodExport = "food.export"

// FoodCapabilityReader retains the current active event and event-scoped roles.
// The domain still authorizes each command or previously bound continuation.
type FoodCapabilityReader interface {
	FoodCapabilities(context.Context, string) (legacyfood.OwnerCapabilities, error)
}

// FoodScriptCatalog owns the whole live food discovery family. The bindings
// supply existing owner/admin callbacks and their original result bounds.
type FoodScriptCatalog struct {
	Client       FoodCapabilityReader
	OwnerBinding ScriptToolEntry
	AdminBinding ScriptToolEntry
}

func (c FoodScriptCatalog) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	capability, err := c.Client.FoodCapabilities(ctx, owner)
	if err != nil || capability.EventID == "" {
		return nil, err
	}
	descriptors := []scriptclient.Tool{
		{
			Name: scriptFoodView,
			Description: "Read your current food event, complete menu, own order and payment instructions as " +
				"JSON chunks. Follow next_cursor and concatenate json. A fresh read is required before " +
				"food changes. Event, owner and versions are host-bound.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"cursor":{"type":"string","maxLength":2048}},"additionalProperties":false}`,
			),
		},
		{
			Name: scriptFoodQuote,
			Description: "Quote meals after food.view; changes nothing. RUB total and completeness are " +
				"authoritative. Meals use menu day keys: " +
				"{friday:{lunch:{type:'individual-items',items:[0]},dinner:[0]}}. Lunch types: " +
				"no-lunch; individual-items with index array; combo-with-soup or combo-no-soup with " +
				"items {soup_index,main_index,side_index,salad_index} (omit soup_index for no-soup). " +
				"Indices are zero-based. Never invent menu items.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"meals":{"type":"object"}},"required":["meals"],"additionalProperties":false}`,
			),
		},
		{
			Name: scriptFoodChange,
			Description: "Change your food order only as requested: save_meals replaces the meal selection, " +
				"delete_meals clears meals, toggle_activity toggles one activity. Requires food.view " +
				"in this turn; host binds event/order/version/replay. Meals and activities have " +
				"independent payment locks. Never submits a receipt or reviews payment.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"name":{"enum":["save_meals","delete_meals","toggle_activity"]},` +
					`"meals":{"type":"object"},"activity":{"type":"string"}},"required":["name"],` +
					`"additionalProperties":false}`,
			),
		},
		{
			Name: scriptFoodPrepare,
			Description: "Prepare payment for your explicitly requested meals or activities after food.view. " +
				"Displays the current food card and receipt prompt. Does not upload, select or submit " +
				"any proof; an unrelated next message remains unrelated.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"kind":{"enum":["meals","activities"]}},"required":["kind"],` +
					`"additionalProperties":false}`,
			),
		},
	}
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(entries, ScriptToolEntry{Descriptor: descriptor, Prepare: c.OwnerBinding.Prepare,
			Execute: c.OwnerBinding.Execute, ResultLimit: c.OwnerBinding.ResultLimit})
	}
	return append(entries, c.adminEntries(capability)...), nil
}

func (c FoodScriptCatalog) adminEntries(capability legacyfood.OwnerCapabilities) []ScriptToolEntry {
	descriptors := []scriptclient.Tool{}
	if capability.CanReview {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name: scriptFoodReviewQueue,
				Description: "List orders with a submitted food payment in the current event. Bounded pages; follow " +
					"next_cursor. Read an order before reviewing it.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"cursor":{"type":"string"}},"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name: scriptFoodReviewRead,
				Description: "Read a food order in your current authorized review event as JSON chunks. Follow " +
					"next_cursor. Binds the observed order/version and separate meal/activity generations " +
					"for explicit review. Proof references remain host-owned.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"order_id":{"type":"string"},"cursor":{"type":"string"}},` +
						`"required":["order_id"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name: scriptFoodReviewDecide,
				Description: "Accept or reject the explicitly requested meals or activities payment after " +
					"food.review.read. Host binds observed order/version/generation; current rights and " +
					"submitted status are rechecked. Never infer acceptance merely from a receipt upload.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"kind":{"enum":["meals","activities"]},"decision":{"enum":["accept",` +
						`"reject"]}},"required":["kind","decision"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name: scriptFoodReviewProof,
				Description: "Display the observed meals or activities proof in this Telegram chat after " +
					"food.review.read. Returns delivery status only, never file bytes or a downloadable " +
					"URL.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"kind":{"enum":["meals","activities"]}},"required":["kind"],` +
						`"additionalProperties":false}`,
				),
			},
		)
	}
	if capability.CanExport {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name: scriptFoodExport,
				Description: "Deliver the current food event's two CSV files to this Telegram chat. No CSV contents " +
					"are returned. Save continuation and pass it to resume incomplete delivery; completed " +
					"files are not resent. Only one export is started per Telegram update. Current export " +
					"rights are checked before each unsent file.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"continuation":{"type":"string"}},"additionalProperties":false}`,
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
				Prepare:     c.AdminBinding.Prepare,
				Execute:     c.AdminBinding.Execute,
				ResultLimit: c.AdminBinding.ResultLimit,
			},
		)
	}
	return entries
}
