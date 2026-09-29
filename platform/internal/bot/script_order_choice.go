package bot

import (
	"context"

	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const modernOrdersChoice = "orders.choice"
const maxChoiceRevisions = 128
const modernChoiceLabelRunes = 80

// Drafts are immutable successful script receipts. Child claims prevent a stale
// branch or consumed create draft from authorizing another command.
type modernChoiceRecord struct {
	HistoryGeneration *int64        `json:"history_generation,omitempty"`
	Ref               string        `json:"ref"`
	Parent            string        `json:"parent,omitempty"`
	Child             string        `json:"child,omitempty"`
	Depth             int           `json:"depth"`
	Event             string        `json:"event"`
	Catalog           string        `json:"catalog"`
	OrderID           string        `json:"order_id,omitempty"`
	Snapshot          string        `json:"snapshot,omitempty"`
	Choice            orders.Choice `json:"choice"`
}

type modernChoiceArguments struct {
	Meals      []modernChoiceMeal         `json:"meals,omitempty"`
	Operation  string                     `json:"operation"`
	Ref        string                     `json:"choice_ref,omitempty"`
	Event      string                     `json:"event,omitempty"`
	OrderID    string                     `json:"order_id,omitempty"`
	Empty      bool                       `json:"empty,omitempty"`
	Part       string                     `json:"part,omitempty"`
	Cursor     string                     `json:"cursor,omitempty"`
	Customer   *string                    `json:"customer,omitempty"`
	FirstName  *string                    `json:"customer_first_name,omitempty"`
	LastName   *string                    `json:"customer_last_name,omitempty"`
	Patronymic *string                    `json:"customer_patronymus,omitempty"`
	Days       map[string]orders.DayInput `json:"days,omitempty"`
	Extras     []modernChoiceExtra        `json:"extras,omitempty"`
}

type modernChoiceExtra struct {
	Ref      int  `json:"ref"`
	Selected bool `json:"selected"`
}

func (b *Bot) modernChoiceEntry() scriptToolEntry {
	return scriptToolEntry{
		descriptor: scriptclient.Tool{
			Name:        modernOrdersChoice,
			Description: "Build a host-owned full order choice across bounded turns. begin: empty=true for create, or order_id after complete orders.inspect; empty=true with order_id replaces the whole choice. patch: exact choice_ref plus customer fields, complete days replacement, or up to 1024 {ref,selected} extra changes. read: part summary/choice/catalog; catalog gives stable extra indexes bound to this draft. Full choice/catalog reads use cursors. Every patch returns a new immutable choice_ref; old revisions cannot branch or commit. Use orders.quote/update with latest choice_ref. No mutation is authorized by an unrelated follow-up.",
			InputSchema: modernOrderSchema(
				`"operation":{"enum":["begin","patch","read"]},"choice_ref":{"type":"string"},"event":{"type":"string"},"order_id":{"type":"string"},"empty":{"type":"boolean"},"part":{"enum":["summary","choice","catalog"]},"cursor":{"type":"string"},"customer":{"type":"string"},"customer_first_name":{"type":"string"},"customer_last_name":{"type":"string"},"customer_patronymus":{"type":"string"},"days":{"type":"object"},"meals":{"type":"array","items":{"type":"object","properties":{"day_ref":{"type":"integer"},"meal_ref":{"type":"integer"},"append":{"type":"boolean"},"remove":{"type":"boolean"},"dishes":{"type":"array","items":{"type":"object","properties":{"ref":{"type":"integer"},"count":{"type":"integer"}},"required":["ref","count"],"additionalProperties":false}}},"required":["day_ref","meal_ref"],"additionalProperties":false}},"extras":{"type":"array","items":{"type":"object","properties":{"ref":{"type":"integer"},"selected":{"type":"boolean"}},"required":["ref","selected"],"additionalProperties":false}}`,
				`"operation"`,
			),
		},
		prepare: b.prepareModernChoice, execute: b.executeModernChoice, resultLimit: maxScriptReadBytes,
	}
}

func decodeModernChoice(call scriptclient.ToolCall) (modernChoiceArguments, error) {
	var args modernChoiceArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return args, err
	}
	if len(args.Ref) > 96 || len(args.Event) > 128 || len(args.OrderID) > 128 || len(args.Cursor) > 2048 ||
		len(args.Extras) > modernChoicePatchItems {
		return args, errors.New("invalid choice arguments")
	}
	if err := validModernMealPatch(args.Meals); err != nil {
		return args, err
	}
	return args, validateModernChoiceOperation(args)
}

func validateModernChoiceOperation(args modernChoiceArguments) error {
	patch := args.Customer != nil || args.FirstName != nil || args.LastName != nil || args.Patronymic != nil ||
		args.Days != nil ||
		len(args.Extras) > 0 || len(args.Meals) > 0
	switch args.Operation {
	case "begin":
		if args.Ref != "" || args.Part != "" || args.Cursor != "" || patch || (args.OrderID == "" && !args.Empty) {
			return errors.New("invalid choice begin")
		}
	case "patch":
		if args.Ref == "" || args.Event != "" || args.OrderID != "" || args.Empty || args.Part != "" ||
			args.Cursor != "" ||
			!patch {
			return errors.New("invalid choice patch")
		}
	case "read":
		if args.Ref == "" || args.Event != "" || args.OrderID != "" || args.Empty || patch {
			return errors.New("invalid choice read")
		}
		if !slices.Contains([]string{"", "summary", "choice", "catalog"}, args.Part) {
			return errors.New("invalid choice part")
		}
	default:
		return errors.New("invalid choice operation")
	}
	return nil
}

func (b *Bot) prepareModernChoice(
	ctx context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	args, err := decodeModernChoice(call)
	if err != nil {
		return record, err
	}
	var draft modernChoiceRecord
	var event orders.Event
	if args.Operation == "begin" {
		draft, event, err = b.beginModernChoice(ctx, owner, args, input)
	} else {
		draft, event, err = b.currentModernChoice(ctx, owner, args.Ref)
	}
	if err != nil {
		return record, err
	}
	if args.Operation == "read" {
		return record, nil
	}
	if args.Operation == "patch" {
		if draft.Depth >= maxChoiceRevisions {
			return record, errors.New("choice revision limit; begin a new draft")
		}
		next, patchErr := patchModernChoice(draft.Choice, event, args)
		if patchErr != nil {
			return record, patchErr
		}
		draft.Parent, draft.Ref, draft.Child = draft.Ref, "", ""
		draft.Depth++
		draft.Choice = next
	}
	record.ModernChoice = &draft
	return record, nil
}

func (b *Bot) beginModernChoice(
	ctx context.Context,
	owner string,
	args modernChoiceArguments,
	input agent.Input,
) (modernChoiceRecord, orders.Event, error) {
	draft := modernChoiceRecord{Event: args.Event, HistoryGeneration: &input.HistoryGeneration}
	if draft.Event == "" {
		draft.Event = b.currentOrderEvent()
	}
	var choice orders.ChoiceInput
	if args.OrderID != "" {
		current, snapshot, err := b.observedModernChoice(ctx, owner, args, input)
		if err != nil {
			return draft, orders.Event{}, err
		}
		draft.Event, draft.OrderID, draft.Snapshot = current.EventID, current.ID, snapshot
		if !args.Empty {
			choice = choiceInput(current.Choice)
		}
	}
	event, err := b.API.OrderEvent(ctx, owner, draft.Event)
	if err != nil {
		return draft, event, err
	}
	draft.Catalog = modernCatalogFingerprint(event)
	draft.Choice, err = b.API.QuoteOrder(ctx, owner, draft.Event, choice)
	return draft, event, err
}

func modernCatalogFingerprint(event orders.Event) string { return orders.CatalogSnapshot(event) }

func patchModernChoice(before orders.Choice, event orders.Event, args modernChoiceArguments) (orders.Choice, error) {
	choice := choiceInput(before)
	fields := []struct {
		value  *string
		target *string
	}{{args.Customer, &choice.Customer}, {args.FirstName, &choice.FirstName}, {args.LastName, &choice.LastName}, {args.Patronymic, &choice.Patronymic}}
	for _, field := range fields {
		if field.value != nil {
			*field.target = *field.value
		}
	}
	if args.Days != nil {
		choice.Days = args.Days
	}
	keys := modernChoiceKeys(event)
	for _, change := range args.Extras {
		if change.Ref < 0 || change.Ref >= len(keys) {
			return orders.Choice{}, errors.New("unknown catalog reference")
		}
		key := keys[change.Ref]
		if change.Selected {
			choice.Extras[key] = json.RawMessage(`0`)
		} else {
			delete(choice.Extras, key)
		}
	}
	var menu orders.Catalog
	if err := json.Unmarshal(event.Menu, &menu); err != nil {
		return orders.Choice{}, err
	}
	if err := patchModernChoiceMeals(&choice, menu, args.Meals); err != nil {
		return orders.Choice{}, err
	}
	return orders.Canonicalize(choice, menu, event.Extras)
}

func modernChoiceKeys(event orders.Event) []string {
	keys := make([]string, 0, len(event.Extras))
	for key := range event.Extras {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (b *Bot) executeModernChoice(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	_ *agent.Input,
) (any, error) {
	args, err := decodeModernChoice(call)
	if err != nil {
		return nil, err
	}
	if record.ModernChoice != nil {
		if err = b.validateModernChoiceGeneration(ctx, owner, *record.ModernChoice); err != nil {
			return nil, err
		}
		return modernChoiceSummary(*record.ModernChoice), nil
	}
	draft, event, err := b.currentModernChoice(ctx, owner, args.Ref)
	if err != nil {
		return nil, err
	}
	cursor, err := core.DecodeReadCursor(args.Cursor, owner, modernOrdersChoice+":"+draft.Ref+":"+args.Part)
	if err != nil {
		return nil, err
	}
	switch args.Part {
	case "choice":
		return core.JSONReadChunk(draft.Choice, cursor)
	case "catalog":
		return modernChoiceCatalogPage(draft, event, cursor)
	default:
		if args.Cursor != "" {
			return nil, errors.New("summary has no cursor")
		}
		return modernChoiceSummary(draft), nil
	}
}

func modernChoiceSummary(draft modernChoiceRecord) any {
	encoded, _ := json.Marshal(draft.Choice)
	return struct {
		Ref      string       `json:"choice_ref"`
		OrderID  string       `json:"order_id,omitempty"`
		Total    orders.Money `json:"total"`
		Bytes    int          `json:"choice_bytes"`
		Complete bool         `json:"choice_complete"`
	}{draft.Ref, draft.OrderID, draft.Choice.Total, len(encoded), false}
}

func modernChoiceCatalogPage(draft modernChoiceRecord, event orders.Event, cursor core.ReadCursor) (any, error) {
	type item struct {
		Ref      int    `json:"ref"`
		Label    string `json:"label"`
		Partial  bool   `json:"label_partial"`
		Selected bool   `json:"selected"`
	}
	keys := modernChoiceKeys(event)
	items := make([]item, 0, len(keys))
	for index, key := range keys {
		label := []rune(key)
		partial := len(label) > modernChoiceLabelRunes
		if partial {
			label = label[:modernChoiceLabelRunes]
		}
		_, selected := draft.Choice.Extras[key]
		items = append(items, item{index, string(label), partial, selected})
	}
	// The full labels remain available through orders.event; these are explicit
	// bounded references, not truncated keys that can be submitted as identities.
	days, err := modernChoiceMealCatalog(event)
	if err != nil {
		return nil, err
	}
	return core.JSONReadChunk(struct {
		Extras []item                 `json:"extras"`
		Days   []modernChoiceDayLabel `json:"days"`
	}{items, days}, cursor)
}

func parseModernChoiceRef(ref string) (int64, int, int, error) {
	var update int64
	var run, call int
	if _, err := fmt.Sscanf(
		ref,
		"%d.%d.%d",
		&update,
		&run,
		&call,
	); err != nil || update <= 0 || run < 0 || run >= agent.MaxScriptRuns || call < 0 || call >= maxScriptCalls ||
		ref != fmt.Sprintf("%d.%d.%d", update, run, call) {
		return 0, 0, 0, errors.New("invalid choice reference")
	}
	return update, run, call, nil
}

func (b *Bot) observedModernChoice(
	ctx context.Context,
	owner string,
	args modernChoiceArguments,
	input agent.Input,
) (orders.Order, string, error) {
	observed := input.ModernOrder
	if observed == nil || input.ModernOrderReview || input.ModernOrderCursor != "" || observed.ID != args.OrderID ||
		(args.Event != "" && args.Event != observed.EventID) {
		return orders.Order{}, "", errors.New("complete matching order read required")
	}
	current, err := b.API.Order(ctx, owner, observed.EventID, observed.ID)
	if err != nil {
		return current, "", err
	}
	snapshot, err := modernOrderFingerprint(current)
	if err != nil {
		return current, "", err
	}
	observedHash, err := modernOrderFingerprint(*observed)
	if err != nil || observedHash != snapshot {
		return current, "", errScriptReadStale
	}
	return current, snapshot, nil
}
