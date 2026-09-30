package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptFoodReviewQueue = "food.review.queue"
const scriptFoodReviewRead = "food.review.read"
const scriptFoodReviewDecide = "food.review.decide"
const scriptFoodReviewProof = "food.review.proof"
const scriptFoodExport = "food.export"

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
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
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
	capability, err := b.API.FoodCapabilities(ctx, owner)
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
	record agenthost.ScriptToolRecord,
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
		err := b.API.Call(
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
		return b.deliverFoodReviewProof(ctx, owner, command, record.Source)
	case scriptFoodReviewDecide:
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		order, err := b.Host.ExecuteDerivedFood(ctx, owner, command, *record.Source)
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
	view, err := b.API.FoodViewForReview(ctx, owner, command.EventID, command.OrderID, true)
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
		return nil, appclient.ReadError(err)
	}
	input.FoodReview = &view
	input.FoodReviewCursor = result.NextCursor
	return result, nil
}

func (b *Bot) deliverFoodReviewProof(
	ctx context.Context,
	owner string,
	command legacyfood.Command,
	derivation *readsource.Derivation,
) (any, error) {
	if derivation == nil || !derivation.Valid() {
		return nil, errors.New("missing admitted source")
	}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return nil, botdelivery.ErrBinding
	}
	family := botFamilyFoodReviewMeals
	if command.Kind == legacyfood.Activity {
		family = botFamilyFoodReviewActivity
	} else if command.Kind != legacyfood.Meals {
		return nil, botdelivery.ErrBinding
	}
	observed, err := b.queueBotDocument(
		ctx,
		owner,
		source.in.chat,
		botdelivery.Reference{
			Family:       family,
			Event:        command.EventID,
			Object:       command.OrderID,
			Version:      command.Version,
			Attempt:      command.Generation,
			Source:       derivation,
			Continuation: botdelivery.Continuation{Kind: botDocumentKind},
		},
	)
	return map[string]any{"displayed": documentDelivered(observed), "delivery_state": observed.State}, err
}
