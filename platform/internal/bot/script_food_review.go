package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptFoodReviewQueue = "food.review.queue"
const scriptFoodReviewRead = "food.review.read"
const scriptFoodReviewDecide = "food.review.decide"
const scriptFoodReviewProof = "food.review.proof"
const scriptFoodExport = "food.export"

func (b *Bot) scriptFoodAdminEntries(capability legacyfood.OwnerCapabilities) []scriptToolEntry {
	descriptors := []scriptclient.Tool{}
	if capability.CanReview {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptFoodReviewQueue,
				Description: "List orders with a submitted food payment in the current event. Bounded pages; follow next_cursor. Read an order before reviewing it.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"cursor":{"type":"string"}},"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptFoodReviewRead,
				Description: "Read a food order in your current authorized review event as JSON chunks. Follow next_cursor. Binds the observed order/version and separate meal/activity generations for explicit review. Proof references remain host-owned.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"order_id":{"type":"string"},"cursor":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptFoodReviewDecide,
				Description: "Accept or reject the explicitly requested meals or activities payment after food.review.read. Host binds observed order/version/generation; current rights and submitted status are rechecked. Never infer acceptance merely from a receipt upload.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"kind":{"enum":["meals","activities"]},"decision":{"enum":["accept","reject"]}},"required":["kind","decision"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptFoodReviewProof,
				Description: "Display the observed meals or activities proof in this Telegram chat after food.review.read. Returns delivery status only, never file bytes or a downloadable URL.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"kind":{"enum":["meals","activities"]}},"required":["kind"],"additionalProperties":false}`,
				),
			},
		)
	}
	if capability.CanExport {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptFoodExport,
				Description: "Deliver the current food event's two CSV files to this Telegram chat. No CSV contents are returned. Save continuation and pass it to resume incomplete delivery; completed files are not resent. Only one export is started per Telegram update. Current export rights are checked before each unsent file.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"continuation":{"type":"string"}},"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]scriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			scriptToolEntry{
				descriptor:  descriptor,
				prepare:     b.prepareFoodAdminTool,
				execute:     b.executeFoodAdminTool,
				resultLimit: maxScriptReadBytes,
			},
		)
	}
	return entries
}

type foodReviewArguments struct {
	OrderID  string `json:"order_id,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Decision string `json:"decision,omitempty"`
}

func validFoodReviewArguments(name string, args foodReviewArguments) bool {
	switch name {
	case scriptFoodReviewQueue:
		return args.OrderID == "" && args.Kind == "" && args.Decision == ""
	case scriptFoodReviewRead:
		return args.OrderID != "" && len(args.OrderID) <= 128 && args.Kind == "" && args.Decision == ""
	case scriptFoodReviewDecide:
		return args.OrderID == "" && args.Cursor == "" && foodPaymentKind(args.Kind) &&
			(args.Decision == passAccept || args.Decision == knowledgeRejectDecision)
	case scriptFoodReviewProof:
		return args.OrderID == "" && args.Cursor == "" && foodPaymentKind(args.Kind) && args.Decision == ""
	default:
		return false
	}
}

func foodPaymentKind(kind string) bool {
	return kind == legacyfood.Meals || kind == legacyfood.Activity
}

func (b *Bot) prepareFoodAdminTool(
	ctx context.Context,
	owner string,
	update int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	if call.Name == scriptFoodExport {
		return b.prepareFoodExport(ctx, owner, update, call, record)
	}
	var args foodReviewArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	if !validFoodReviewArguments(call.Name, args) {
		return record, errors.New("invalid food review arguments")
	}
	capability, err := b.API.foodCapabilities(ctx, owner)
	if err != nil {
		return record, err
	}
	if !capability.CanReview {
		return record, errors.New("food review unavailable")
	}
	record.Food = &legacyfood.Command{EventID: capability.EventID, OrderID: args.OrderID}
	if call.Name == scriptFoodReviewQueue || call.Name == scriptFoodReviewRead {
		return record, nil
	}
	view := input.FoodReview
	if view == nil || input.FoodReviewCursor != "" || view.Event.ID != capability.EventID {
		return record, errors.New("current food review required")
	}
	payment := view.Order.MealPayment
	if args.Kind == legacyfood.Activity {
		payment = view.Order.ActivityPayment
	}
	record.Food = &legacyfood.Command{
		EventID:    view.Event.ID,
		OrderID:    view.Order.ID,
		Version:    view.Order.Version,
		Kind:       args.Kind,
		Generation: payment.Generation,
		Name:       args.Decision,
	}
	return record, nil
}

func (b *Bot) executeFoodAdminTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	input *agent.Input,
) (any, error) {
	if call.Name == scriptFoodExport {
		return b.executeFoodExport(ctx, owner, record)
	}
	if record.Food == nil {
		return nil, errors.New("food review binding missing")
	}
	command := *record.Food
	var args foodReviewArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	switch call.Name {
	case scriptFoodReviewQueue:
		var result core.ReadPage[legacyfood.ReviewItem]
		err := b.API.call(
			ctx,
			owner,
			http.MethodGet,
			"/v1/food/review-queue?event="+url.QueryEscape(command.EventID)+"&cursor="+url.QueryEscape(args.Cursor),
			nil,
			&result,
		)
		return result, err
	case scriptFoodReviewRead:
		return b.readFoodReview(ctx, owner, command, args.Cursor, input)
	case scriptFoodReviewProof:
		return b.deliverFoodReviewProof(ctx, owner, command)
	case scriptFoodReviewDecide:
		order, err := b.API.ExecuteFood(ctx, owner, command)
		if err != nil {
			return nil, err
		}
		input.FoodReview = &legacyfood.View{Event: legacyfood.Event{ID: order.EventID}, Order: order}
		return scriptFoodOrderSummary(order), nil
	default:
		return nil, errors.New("unknown food review tool")
	}
}

func (b *Bot) readFoodReview(
	ctx context.Context,
	owner string,
	command legacyfood.Command,
	raw string,
	input *agent.Input,
) (any, error) {
	valid := raw == "" || (input.FoodReview != nil && input.FoodReview.Event.ID == command.EventID &&
		input.FoodReview.Order.ID == command.OrderID && raw == input.FoodReviewCursor)
	input.FoodReview = nil
	input.FoodReviewCursor = ""
	if !valid {
		return nil, errors.New("food review continuation unavailable; read again")
	}
	view, err := b.API.foodView(ctx, owner, command.EventID, command.OrderID, true)
	if err != nil {
		return nil, err
	}
	cursor, err := core.DecodeReadCursor(raw, owner, scriptFoodReviewRead+":"+command.EventID+":"+command.OrderID)
	if err != nil {
		return nil, err
	}
	safe := view.Order
	safe.MealPayment.ProofID, safe.MealPayment.ProofSource, safe.MealPayment.LegacySourceKey = "", "", ""
	safe.ActivityPayment.ProofID, safe.ActivityPayment.ProofSource, safe.ActivityPayment.LegacySourceKey = "", "", ""
	result, err := core.JSONReadChunk(safe, cursor)
	if err != nil {
		return nil, scriptDomainAPIError(err)
	}
	input.FoodReview = &view
	input.FoodReviewCursor = result.NextCursor
	return result, nil
}

func (b *Bot) deliverFoodReviewProof(ctx context.Context, owner string, command legacyfood.Command) (any, error) {
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return nil, errors.New("food delivery context missing")
	}
	view, err := b.API.foodView(ctx, owner, command.EventID, command.OrderID, true)
	if err != nil {
		return nil, err
	}
	if view.Order.Version != command.Version {
		return nil, errors.New("food review changed; read again")
	}
	body, err := b.API.foodReviewProof(ctx, owner, command)
	if err != nil {
		return nil, err
	}
	_, err = b.TG.SendDocument(ctx, source.in.chat, "receipt", body)
	return map[string]bool{"displayed": err == nil}, err
}
