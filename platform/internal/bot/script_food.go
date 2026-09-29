package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptFoodView = "food.view"
const scriptFoodQuote = "food.quote"
const scriptFoodChange = "food.change"
const scriptFoodPrepare = "food.payment.prepare"
const scriptFoodSaveMeals = "save_meals"
const scriptFoodDeleteMeals = "delete_meals"
const scriptFoodToggle = "toggle_activity"
const maxFoodActivityBytes = 64

func (c APIClient) foodCapabilities(ctx context.Context, owner string) (legacyfood.OwnerCapabilities, error) {
	var result legacyfood.OwnerCapabilities
	err := c.call(ctx, owner, http.MethodGet, "/v1/food/capabilities", nil, &result)
	return result, err
}

func (b *Bot) scriptFoodEntries(ctx context.Context, owner string) ([]scriptToolEntry, error) {
	capability, err := b.API.foodCapabilities(ctx, owner)
	if err != nil || capability.EventID == "" {
		return nil, err
	}
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptFoodView,
			Description: "Read your current food event, complete menu, own order and payment instructions as JSON chunks. Follow next_cursor and concatenate json. A fresh read is required before food changes. Event, owner and versions are host-bound.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"cursor":{"type":"string","maxLength":2048}},"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptFoodQuote,
			Description: "Quote meals after food.view; changes nothing. RUB total and completeness are authoritative. Meals use menu day keys: {friday:{lunch:{type:'individual-items',items:[0]},dinner:[0]}}. Lunch types: no-lunch; individual-items with index array; combo-with-soup or combo-no-soup with items {soup_index,main_index,side_index,salad_index} (omit soup_index for no-soup). Indices are zero-based. Never invent menu items.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"meals":{"type":"object"}},"required":["meals"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptFoodChange,
			Description: "Change your food order only as requested: save_meals replaces the meal selection, delete_meals clears meals, toggle_activity toggles one activity. Requires food.view in this turn; host binds event/order/version/replay. Meals and activities have independent payment locks. Never submits a receipt or reviews payment.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"name":{"enum":["save_meals","delete_meals","toggle_activity"]},"meals":{"type":"object"},"activity":{"type":"string"}},"required":["name"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptFoodPrepare,
			Description: "Prepare payment for your explicitly requested meals or activities after food.view. Displays the current food card and receipt prompt. Does not upload, select or submit any proof; an unrelated next message remains unrelated.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"kind":{"enum":["meals","activities"]}},"required":["kind"],"additionalProperties":false}`,
			),
		},
	}
	entries := make([]scriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(entries, scriptToolEntry{descriptor: descriptor, prepare: b.prepareFoodTool,
			execute: b.executeFoodTool, resultLimit: maxScriptReadBytes})
	}
	return append(entries, b.scriptFoodAdminEntries(capability)...), nil
}

type scriptFoodArguments struct {
	Name     string                   `json:"name,omitempty"`
	Meals    legacyfood.MealSelection `json:"meals,omitempty"`
	Activity string                   `json:"activity,omitempty"`
	Kind     string                   `json:"kind,omitempty"`
}

func (b *Bot) prepareFoodTool(
	ctx context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	if call.Name == scriptFoodView {
		return record, decodeScriptArguments(call.Arguments, new(scriptReadArguments))
	}
	var args scriptFoodArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	if err := validateFoodToolArguments(call.Name, args); err != nil {
		return record, err
	}
	if call.Name == scriptFoodPrepare {
		source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
		if !ok || source.owner != owner {
			return record, errors.New("food delivery context missing")
		}
	}
	view := input.FoodView
	if view == nil || view.Order.Owner != owner {
		return record, errors.New("current food view required")
	}
	if err := b.currentFoodToolEvent(ctx, owner, view.Event.ID); err != nil {
		return record, err
	}
	record.Food = &legacyfood.Command{EventID: view.Event.ID, OrderID: view.Order.ID,
		Version: view.Order.Version, Name: args.Name, Meals: args.Meals, Activity: args.Activity, Kind: args.Kind}
	var err error
	record.Food.CatalogRevision, err = legacyfood.CatalogRevision(view.Event)
	if err != nil {
		return record, err
	}
	if call.Name == scriptFoodPrepare {
		record.Food.Name = foodBeginPayment
		payment := view.Order.MealPayment
		if args.Kind == legacyfood.Activity {
			payment = view.Order.ActivityPayment
		}
		record.Food.Generation = payment.Generation
	}
	return record, nil
}

func validateFoodToolArguments(name string, args scriptFoodArguments) error {
	valid := false
	switch name {
	case scriptFoodQuote:
		valid = args.Meals != nil && args.Name == "" && args.Activity == "" && args.Kind == ""
	case scriptFoodPrepare:
		valid = (args.Kind == legacyfood.Meals || args.Kind == legacyfood.Activity) && args.Name == "" &&
			args.Meals == nil &&
			args.Activity == ""
	case scriptFoodChange:
		if args.Kind != "" {
			break
		}
		switch args.Name {
		case scriptFoodSaveMeals:
			valid = args.Meals != nil && args.Activity == ""
		case scriptFoodDeleteMeals:
			valid = args.Meals == nil && args.Activity == ""
		case scriptFoodToggle:
			valid = args.Meals == nil && args.Activity != "" && len(args.Activity) <= maxFoodActivityBytes
		}
	}
	if !valid {
		return errors.New("invalid food tool arguments")
	}
	return nil
}

func (b *Bot) currentFoodToolEvent(ctx context.Context, owner, event string) error {
	capability, err := b.API.foodCapabilities(ctx, owner)
	if err != nil {
		return err
	}
	if event == "" || capability.EventID != event {
		return errors.New("current food event unavailable; read again")
	}
	return nil
}

func (b *Bot) executeFoodTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	input *agent.Input,
) (any, error) {
	if call.Name == scriptFoodView {
		return b.readFoodTool(ctx, owner, call, input)
	}
	if record.Food == nil {
		return nil, errors.New("food command missing")
	}
	command := *record.Food
	if call.Name == scriptFoodQuote {
		var quote legacyfood.MealQuote
		err := b.API.call(ctx, owner, http.MethodPost, "/v1/food/quote", map[string]any{
			foodEventIDField:   command.EventID,
			legacyfood.Meals:   command.Meals,
			"catalog_revision": command.CatalogRevision,
		}, &quote)
		return quote, err
	}
	order, err := b.API.ExecuteFood(ctx, owner, command)
	if err != nil {
		return nil, err
	}
	if input.FoodView != nil {
		input.FoodView.Order = order
	}
	if call.Name == scriptFoodPrepare {
		source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
		if !ok || source.owner != owner {
			return nil, errors.New("food delivery context missing")
		}
		if err = b.finishOrderedFoodCommand(
			ctx,
			source.in,
			source.update.ID,
			command,
			order,
			record.FoodSequence,
		); err != nil {
			return nil, err
		}
	}
	return scriptFoodOrderSummary(order), nil
}

// Mutation results are bounded independently of the size of the meal selection.
// Complete order/menu data remains available through the chunked view tool.
func scriptFoodOrderSummary(order legacyfood.Order) any {
	return struct {
		ID                 string            `json:"id"`
		EventID            string            `json:"event_id"`
		Version            int64             `json:"version"`
		Complete           bool              `json:"complete"`
		MealTotal          legacyfood.Amount `json:"meal_total"`
		ActivityTotal      legacyfood.Amount `json:"activity_total"`
		MealStatus         string            `json:"meal_status"`
		ActivityStatus     string            `json:"activity_status"`
		MealGeneration     int64             `json:"meal_generation"`
		ActivityGeneration int64             `json:"activity_generation"`
	}{
		order.ID,
		order.EventID,
		order.Version,
		order.Complete,
		order.MealTotal,
		order.ActivityTotal,
		order.MealPayment.Status,
		order.ActivityPayment.Status,
		order.MealPayment.Generation,
		order.ActivityPayment.Generation,
	}
}

func (b *Bot) readFoodTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	input *agent.Input,
) (any, error) {
	input.FoodView = nil
	var args scriptReadArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	view, err := b.API.FoodView(ctx, owner, "", "")
	if err != nil {
		return nil, err
	}
	cursor, err := core.DecodeReadCursor(args.Cursor, owner, scriptFoodView+":"+view.Event.ID)
	if err != nil {
		return nil, err
	}
	result, err := core.JSONReadChunk(view, cursor)
	if err != nil {
		return nil, scriptDomainAPIError(err)
	}
	input.FoodView = &view
	return result, nil
}
