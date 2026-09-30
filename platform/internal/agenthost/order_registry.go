package agenthost

import (
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// OrderScriptCatalog owns modern-order visibility and schema policy. The
// existing registry supplies its freshly authorized business capabilities.
// Bindings retain separate order and choice preparation/execution callbacks.
type OrderScriptCatalog struct {
	OrderBinding  ScriptToolEntry
	ChoiceBinding ScriptToolEntry
}

const (
	modernOrdersEvents       = "orders.events"
	modernOrdersBrowse       = "orders.browse"
	modernOrdersEvent        = "orders.event"
	modernOrdersContacts     = "orders.contacts"
	modernOrdersHistory      = "orders.history.page"
	modernOrdersHistoryRead  = "orders.history.read"
	modernOrdersInspect      = "orders.inspect"
	modernOrdersInstructions = "orders.instructions"
	modernOrdersProof        = "orders.proof"
	modernOrdersQuote        = "orders.quote"
	modernOrdersUpdate       = "orders.update"
	modernOrdersInbox        = "orders.inbox"
	modernOrdersReviewRead   = "orders.review.read"
	modernOrdersReviewDecide = "orders.review.decide"
	modernOrdersReviewProof  = "orders.review.proof"
	modernOrdersExport       = "orders.export"
)

const modernOrdersChoice = "orders.choice"

func modernOrderSchema(properties, required string) json.RawMessage {
	return json.RawMessage(
		`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`,
	)
}

func (c OrderScriptCatalog) Entries(capability core.BusinessCapabilities) []ScriptToolEntry {
	const event = `"event":{"type":"string"}`
	const cursor = `"cursor":{"type":"string"}`
	const resume = `"resume":{"type":"boolean","description":"Replay the last successful page after a new turn or restart; do not combine with cursor."}`
	const order = `"order_id":{"type":"string"}`
	const choice = `"choice_ref":{"type":"string"},"choice":{"type":"object","description":"Full choice: customer, customer_first_name, customer_last_name, customer_patronymus; days maps day keys to {mealtimes:{meal:{dishes:[{name,count}]}}}; extras maps selected extra keys to 0. Use event catalog names. Core recomputes all prices."}`
	page := modernOrderSchema(event+","+cursor, "")
	read := modernOrderSchema(event+","+order+","+cursor+","+resume, `"order_id"`)
	descriptors := []scriptclient.Tool{
		{
			Name:        modernOrdersBrowse,
			Description: "Page owner order summaries for the selected modern event. Follow next_cursor. Use orders.inspect for complete details.",
			InputSchema: page,
		},
		{
			Name:        modernOrdersEvents,
			Description: "Page the authenticated modern-order event catalog. Follow next_cursor while more is true.",
			InputSchema: modernOrderSchema(cursor, ""),
		},
		{
			Name:        modernOrdersEvent,
			Description: "Read complete modern event/menu as JSON chunks. Concatenate json until more=false. Optional event defaults to active event; stale means discard and restart.",
			InputSchema: page,
		},
		{
			Name:        modernOrdersContacts,
			Description: "Read payment contacts as JSON chunks. Contacts do not grant review rights.",
			InputSchema: page,
		},
		{
			Name:        modernOrdersHistory,
			Description: "Page all retained owner order history, including deleted orders. Follow next_cursor; orders.history.read returns complete entry details.",
			InputSchema: page,
		},
		{
			Name:        modernOrdersHistoryRead,
			Description: "Read an owner history entry as JSON chunks; concatenate until complete.",
			InputSchema: modernOrderSchema(event+","+cursor+`,"entry":{"type":"string"}`, `"entry"`),
		},
		{
			Name:        modernOrdersInspect,
			Description: "Read your full selected order as bounded JSON chunks. Follow next_cursor across turns; resume:true replays the last durable page after restart. Offset is a rune index for deduplication. Complete reads bind observed version/payment attempt for orders.update and payment tools. Stale means restart without cursor/resume. No proof IDs are returned.",
			InputSchema: read,
		},
		{
			Name:        modernOrdersInstructions,
			Description: "Read payment instructions as JSON chunks for the order completed by orders.inspect.",
			InputSchema: modernOrderSchema(order+","+cursor, `"order_id"`),
		},
		{
			Name:        modernOrdersProof,
			Description: "Display the receipt bound by completed orders.inspect in this chat. No receipt IDs or bytes are accepted or returned. Uncertain delivery is never automatically resent.",
			InputSchema: modernOrderSchema(order, `"order_id"`),
		},
	}
	if capability.CanBook {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        modernOrdersQuote,
				Description: "Canonicalize and quote a full choice for a selected event. Returns JSON chunks; quote is not a reservation.",
				InputSchema: modernOrderSchema(event+","+choice+","+cursor, ""),
			},
			scriptclient.Tool{
				Name:        modernOrdersUpdate,
				Description: "Perform the user's explicit modern-order request: create with full choice, or edit/delete/cash/cancel_proof/country after completed orders.inspect. Existing order_id must match observation; explicitly select when multiple orders exist. Host binds event, version, attempt, origin and replay key. Uploading receipts remains host-owned.",
				InputSchema: modernOrderSchema(
					event+","+order+","+choice+`,"name":{"enum":["create","edit","delete","cash","cancel_proof","country"]},"country":{"enum":["be","ru"]},"contact":{"type":"string"}`,
					`"name"`,
				),
			},
		)
	}
	if capability.CanExportOrders {
		descriptors = append(descriptors, modernOrderAdminDescriptors(cursor, order, resume)...)
	}
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     c.OrderBinding.Prepare,
				Execute:     c.OrderBinding.Execute,
				ResultLimit: c.OrderBinding.ResultLimit,
			},
		)
	}
	if capability.CanBook {
		entries = append(entries, c.choiceEntry())
	}
	return entries
}

func modernOrderAdminDescriptors(cursor, order, resume string) []scriptclient.Tool {
	return []scriptclient.Tool{
		scriptclient.Tool{
			Name:        modernOrdersInbox,
			Description: "Page the current event's payment inbox. Read each selected order with orders.review.read before an explicit decision.",
			InputSchema: modernOrderSchema(cursor, ""),
		},
		scriptclient.Tool{
			Name:        modernOrdersReviewRead,
			Description: "Read a current event payment as bounded JSON chunks. Follow next_cursor across turns; resume:true replays the last durable page with rune offset. Fresh rights and unchanged snapshot are required. Complete read binds exact version and payment attempt; receipt presence never implies acceptance.",
			InputSchema: modernOrderSchema(order+","+cursor+","+resume, `"order_id"`),
		},
		scriptclient.Tool{
			Name:        modernOrdersReviewDecide,
			Description: "Perform an explicit accept/reject decision for the order completed by orders.review.read. Rights, order version and payment attempt are checked again in Core's transaction.",
			InputSchema: modernOrderSchema(order+`,"name":{"enum":["accept","reject"]}`, `"order_id","name"`),
		},
		scriptclient.Tool{
			Name:        modernOrdersReviewProof,
			Description: "Display the receipt observed by completed orders.review.read in this chat. Replacement is stale. Uncertain sends are not automatically retried.",
			InputSchema: modernOrderSchema(order, `"order_id"`),
		},
		scriptclient.Tool{
			Name:        modernOrdersExport,
			Description: "Deliver the current modern event's exact XLSX to this chat. One admitted attempt per update; uncertain sends are not retried. This is not the legacy food CSV export.",
			InputSchema: modernOrderSchema("", ""),
		},
	}
}

func (c OrderScriptCatalog) choiceEntry() ScriptToolEntry {
	return ScriptToolEntry{
		Descriptor: scriptclient.Tool{
			Name:        modernOrdersChoice,
			Description: "Build a host-owned full order choice across bounded turns. begin: empty=true for create, or order_id after complete orders.inspect; empty=true with order_id replaces the whole choice. event, order_id and empty are begin-only; omit them for read/patch, whose event is bound by choice_ref. patch: exact choice_ref plus customer fields, complete days replacement, or up to 1024 {ref,selected} extra changes. read: part summary/choice/catalog; catalog gives stable extra indexes bound to this draft. Full choice/catalog reads use cursors. Every patch returns a new immutable choice_ref; old revisions cannot branch or commit. Use orders.quote/update with latest choice_ref. No mutation is authorized by an unrelated follow-up.",
			InputSchema: modernOrderSchema(
				`"operation":{"enum":["begin","patch","read"]},"choice_ref":{"type":"string"},"event":{"type":"string"},"order_id":{"type":"string"},"empty":{"type":"boolean"},"part":{"enum":["summary","choice","catalog"]},"cursor":{"type":"string"},"customer":{"type":"string"},"customer_first_name":{"type":"string"},"customer_last_name":{"type":"string"},"customer_patronymus":{"type":"string"},"days":{"type":"object"},"meals":{"type":"array","items":{"type":"object","properties":{"day_ref":{"type":"integer"},"meal_ref":{"type":"integer"},"append":{"type":"boolean"},"remove":{"type":"boolean"},"dishes":{"type":"array","items":{"type":"object","properties":{"ref":{"type":"integer"},"count":{"type":"integer"}},"required":["ref","count"],"additionalProperties":false}}},"required":["day_ref","meal_ref"],"additionalProperties":false}},"extras":{"type":"array","items":{"type":"object","properties":{"ref":{"type":"integer"},"selected":{"type":"boolean"}},"required":["ref","selected"],"additionalProperties":false}}`,
				`"operation"`,
			),
		},
		Prepare: c.ChoiceBinding.Prepare, Execute: c.ChoiceBinding.Execute, ResultLimit: c.ChoiceBinding.ResultLimit,
	}
}
