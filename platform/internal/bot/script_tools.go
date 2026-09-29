package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const maxScriptCalls = 8
const scriptDiscoveryList = "$list"
const scriptToolsTimeout = 7 * time.Second
const scriptInterrupted = "interrupted"
const scriptWorkflowView = "workflow"
const scriptWorkflowGet = "workflow.get"
const scriptWorkflowCatalog = "workflow.catalog"
const scriptOrdersList = "orders.list"
const scriptOrdersGet = "orders.get"
const scriptWorkflowSelect = "workflow.select"
const scriptOrdersChange = "orders.change"

// Commands are bound by the host before admission, never reconstructed from a
// worker result. An interrupted admission is evidence of an uncertain outcome.
type scriptToolRecord struct {
	ChoiceGeneration *int64                  `json:"choice_generation,omitempty"`
	ChoiceCatalog    string                  `json:"choice_catalog,omitempty"`
	ModernChoice     *modernChoiceRecord     `json:"modern_choice,omitempty"`
	ChoiceUse        string                  `json:"choice_use,omitempty"`
	ModernOrder      *modernOrderRequest     `json:"modern_order,omitempty"`
	CreditPolicy     *creditToolCommand      `json:"credit_policy,omitempty"`
	Pass             *scriptPassRequest      `json:"pass,omitempty"`
	BroadcastReview  *broadcastReviewRequest `json:"broadcast_review,omitempty"`
	FoodExport       *foodExportRequest      `json:"food_export,omitempty"`
	FoodSequence     int                     `json:"food_sequence,omitempty"`
	Food             *legacyfood.Command     `json:"food,omitempty"`
	Model            *scriptModelRequest     `json:"model,omitempty"`
	Massage          *scriptMassageRequest   `json:"massage,omitempty"`
	Broadcast        *broadcastToolRequest   `json:"broadcast,omitempty"`
	Memory           *knowledge.Command      `json:"memory,omitempty"`
	Order            *orders.Command         `json:"order,omitempty"`
	Action           *core.Action            `json:"action,omitempty"`
	Outcome          agent.ScriptToolResult  `json:"outcome"`
	Profile          *scriptProfileMutation  `json:"profile,omitempty"`
	Language         *scriptLanguageMutation `json:"language,omitempty"`
}

func scriptTools(canBook bool) []scriptclient.Tool {
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	tools := []scriptclient.Tool{
		{Name: scriptWorkflowGet, Description: "Read your current workflow.", InputSchema: empty},
		{Name: scriptWorkflowCatalog, Description: "Read available workflow slots.", InputSchema: empty},
		{
			Name:        scriptWorkflowSelect,
			Description: "Select a known workflow slot as a draft. Confirmation stays manual.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"slot_id":{"type":"string"}},"required":["slot_id"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptOrdersList,
			Description: "Read summaries of your orders for the active event.",
			InputSchema: empty,
		},
		{
			Name:        scriptOrdersGet,
			Description: "Read one of your order summaries.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptOrdersChange,
			Description: "Create your order or add/remove an extra. Existing order selection must be grounded in the current user request. Host owns identity, event, version and replay key.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"name":{"enum":["create","add_extra","remove_extra"]},"order_id":{"type":"string"},"extra":{"type":"string"}},"required":["name"],"additionalProperties":false}`,
			),
		},
	}
	if !canBook {
		tools = slices.DeleteFunc(tools, func(tool scriptclient.Tool) bool {
			return tool.Name == scriptWorkflowSelect || tool.Name == scriptOrdersChange
		})
	}
	return tools
}

func (b *Bot) evaluateScriptTools(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	p agent.ScriptProposal,
	input *agent.Input,
) (agent.ScriptRun, error) {
	executor, ok := b.Scripts.(scriptExecutor)
	if !ok {
		return b.evaluateScript(ctx, p)
	}
	run := agent.ScriptRun{Code: p.Code}
	// Authenticate before exposing even the ordinary-user tool catalog.
	tools, err := b.availableScriptTools(ctx, owner)
	if err != nil {
		return run, err
	}
	callCtx, cancel := context.WithTimeout(ctx, scriptToolsTimeout)
	defer cancel()
	scope := scriptRunScope(tools)
	result, err := executor.Execute(
		callCtx,
		scriptclient.Request{Code: p.Code, Input: json.RawMessage(p.InputJSON)},
		scriptBindings(tools),
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			ctx = context.WithValue(ctx, scriptRunScopeKey{}, scope)
			return b.callScriptTool(ctx, owner, updateID, index, call, input)
		},
	)
	if ctx.Err() != nil {
		return run, ctx.Err()
	}
	if err != nil || callCtx.Err() != nil {
		run.Error = scriptFailure(callCtx, err)
		return run, nil
	}
	if run.Error = scriptResultError(result); run.Error != "" {
		return run, nil
	}
	run.Result = result
	return run, nil
}

func decodeScriptArguments(raw json.RawMessage, target any) error {
	if len(raw) == 0 || len(raw) > agent.MaxScriptInputBytes || raw[0] != '{' {
		return errors.New("invalid tool arguments")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid tool arguments")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("invalid tool arguments")
	}
	return nil
}

func (b *Bot) prepareWorkflowOrderTool(
	ctx context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	switch call.Name {
	case scriptWorkflowGet, scriptWorkflowCatalog, scriptOrdersList:
		return record, decodeScriptArguments(call.Arguments, &struct{}{})
	case scriptOrdersGet:
		var args struct {
			OrderID string `json:"order_id"`
		}
		if err := decodeScriptArguments(
			call.Arguments,
			&args,
		); err != nil || args.OrderID == "" ||
			len(args.OrderID) > 64 {
			return record, errors.New("invalid tool arguments")
		}
		return record, nil
	case scriptWorkflowSelect:
		var args struct {
			SlotID string `json:"slot_id"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return record, err
		}
		plan := agent.Plan{View: scriptWorkflowView, Action: &agent.Proposal{Name: "select", SlotID: args.SlotID}}
		if err := agent.Validate(plan); err != nil {
			return record, err
		}
		if !slices.ContainsFunc(input.Catalog, func(slot core.Slot) bool { return slot.ID == args.SlotID }) {
			return record, errors.New("unknown resource")
		}
		record.Action = &core.Action{
			Name:    "select",
			SlotID:  args.SlotID,
			Version: input.Workflow.Version,
			Origin:  originAgent,
		}
		return record, nil
	case scriptOrdersChange:
		var proposal agent.OrderProposal
		if err := decodeScriptArguments(call.Arguments, &proposal); err != nil {
			return record, err
		}
		if proposal.Name != actionCreateOrder && proposal.Name != "add_extra" && proposal.Name != "remove_extra" {
			return record, errors.New("invalid tool arguments")
		}
		if err := agent.Validate(agent.Plan{View: agent.OrdersView, OrderAction: &proposal}); err != nil {
			return record, err
		}
		command, err := b.proposedOrderCommand(ctx, owner, &proposal, input)
		record.Order = command
		return record, err
	default:
		return record, errors.New("tool unavailable")
	}
}

func (b *Bot) callScriptTool(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	call scriptclient.ToolCall,
	input *agent.Input,
) (json.RawMessage, error) {
	if call.Name == scriptProfileSet {
		if err := b.markPrivateProfileScript(ctx, owner, updateID, index); err != nil {
			return nil, err
		}
	}
	operation := call.Name
	if call.Name == scriptDiscoveryList || call.Name == "$help" {
		operation = "discovery"
	}
	ctx, diagnostic := observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: "tool", Operation: operation, InputBytes: len(call.Arguments)})
	output, err := b.callScriptToolObserved(ctx, owner, updateID, index, call, input, diagnostic)
	clearUncommittedModernOrderRead(input, call.Name, err == nil)
	count, empty := scriptToolResultMetadata(call.Name, output)
	diagnostic.Result(len(output), count, empty && err == nil)
	diagnostic.Finish(err)
	return output, err
}

func (b *Bot) callScriptToolObserved(ctx context.Context, owner string, updateID int64, index int,
	call scriptclient.ToolCall, input *agent.Input, diagnostic *observability.AgentSpan) (json.RawMessage, error) {
	if call.Name == scriptDiscoveryList || call.Name == "$help" {
		return b.discoverScriptTools(ctx, owner, call)
	}

	entries, err := b.authorizedScriptRegistry(ctx, owner)
	if err != nil {
		return nil, errors.New("tool unavailable")
	}
	position := slices.IndexFunc(
		entries,
		func(entry scriptToolEntry) bool { return entry.descriptor.Name == call.Name },
	)
	if position < 0 {
		diagnostic.Outcome("denied", "unavailable")
		return nil, errors.New("tool unavailable")
	}
	entry := entries[position]
	record, err := entry.prepare(ctx, owner, updateID, call, *input)
	if err != nil {
		// Preparation also reads fresh domain state; an error is not proof of bad model input.
		diagnostic.Outcome("error", "unavailable")
		return nil, errors.New("tool unavailable or invalid arguments")
	}
	sequence, err := b.reserveScriptTool(ctx, owner, updateID, index, &record)
	if err != nil {
		return nil, err
	}
	result, err := entry.execute(ctx, owner, call, record, input)
	outcomeError := ""
	if errors.Is(err, errScriptReadStale) {
		result = json.RawMessage(`{"error":"stale","restart":true}`)
		err = nil
		outcomeError = "stale"
		diagnostic.Outcome("error", "conflict")
	}
	if errors.Is(err, errScriptReadLimit) {
		result = json.RawMessage(`{"error":"result_limit"}`)
		err = nil
		outcomeError = "result_limit"
		diagnostic.Outcome("limited", "result_limit")
	}
	if err != nil {
		var problem *core.ProblemError
		if errors.As(err, &problem) && problem.Status < 500 {
			diagnostic.Outcome("denied", "unavailable")
			record.Outcome.Error = "denied"
			if saveErr := b.finishScriptTool(ctx, owner, updateID, index, sequence, record); saveErr != nil {
				return nil, saveErr
			}
		}
		return nil, errors.New("tool execution failed; inspect host call outcomes")
	}
	body, err := json.Marshal(result)
	limit := entry.resultLimit
	if err != nil || len(body) > limit {
		diagnostic.Outcome("limited", "result_limit")
		return nil, errors.New("tool result exceeds limit; inspect current state")
	}
	record.Outcome.Result = body
	record.Outcome.Error = outcomeError
	if err = b.finishScriptTool(ctx, owner, updateID, index, sequence, record); err != nil {
		return nil, err
	}
	return body, nil
}

func (b *Bot) discoverScriptTools(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
) (json.RawMessage, error) {
	tools, err := b.availableScriptTools(ctx, owner)
	if err != nil {
		return nil, errors.New("tool unavailable")
	}
	if call.Name == scriptDiscoveryList {
		if err = decodeScriptArguments(call.Arguments, &struct{}{}); err != nil {
			return nil, err
		}
		type summary struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		list := make([]summary, 0)
		for _, tool := range tools {
			list = append(list, summary{Name: tool.Name, Description: tool.Description})
		}
		return json.Marshal(list)
	}
	var args struct {
		Name string `json:"name"`
	}
	if err = decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	for _, tool := range tools {
		if tool.Name == args.Name {
			return json.Marshal(tool)
		}
	}
	return nil, errors.New("tool unavailable")
}

func (b *Bot) executeWorkflowOrderTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.Action != nil {
		result, err := b.API.Execute(ctx, owner, *record.Action)
		if err == nil {
			input.Workflow = result
		}
		return result, err
	}
	if record.Order != nil {
		result, err := b.API.ExecuteOrder(ctx, owner, *record.Order)
		if err != nil {
			return nil, err
		}
		summary := scriptOrderSummary(result)
		position := slices.IndexFunc(input.Orders, func(item agent.OrderSummary) bool { return item.ID == result.ID })
		if position >= 0 {
			input.Orders[position] = summary
		} else {
			input.Orders = append(input.Orders, summary)
			input.OrderCount++
			input.EditableOrderCount++
		}
		return summary, nil
	}
	switch call.Name {
	case scriptWorkflowGet:
		result, err := b.API.Current(ctx, owner)
		if err == nil {
			input.Workflow = result
		}
		return result, err
	case scriptWorkflowCatalog:
		result, err := b.API.Catalog(ctx, owner)
		if err == nil {
			input.Catalog = result
		}
		return result, err
	case scriptOrdersList:
		return b.readScriptOrders(ctx, owner, "", input)
	case scriptOrdersGet:
		var args struct {
			OrderID string `json:"order_id"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return nil, err
		}
		return b.readScriptOrders(ctx, owner, args.OrderID, input)
	default:
		return nil, errors.New("tool unavailable")
	}
}

// Use a complete owner-scoped snapshot for counts, even for a single-item read.
// Input.Orders can be a selected subset and cannot establish ambiguity alone.
func (b *Bot) readScriptOrders(ctx context.Context, owner, selected string, input *agent.Input) (any, error) {
	list, err := b.API.Orders(ctx, owner, b.currentOrderEvent())
	if err != nil {
		return nil, err
	}
	if selected != "" && !slices.ContainsFunc(list, func(order orders.Order) bool { return order.ID == selected }) {
		return nil, errors.New("tool unavailable")
	}
	input.OrderCount = len(list)
	input.EditableOrderCount = 0
	result := make([]agent.OrderSummary, 0, len(list))
	for _, order := range list {
		if order.State == stateUnpaid || order.State == stateCash {
			input.EditableOrderCount++
		}
		result = append(result, scriptOrderSummary(order))
	}
	input.Orders = orderSummaries(list, currentRequestEvidence(*input)+" "+selected, input.EditableOrderCount)
	if selected != "" {
		for _, summary := range result {
			if summary.ID == selected {
				return summary, nil
			}
		}
	}
	return result, nil
}

func scriptOrderSummary(order orders.Order) agent.OrderSummary {
	return agent.OrderSummary{
		ID:      order.ID,
		Version: order.Version,
		State:   order.State,
		Choice:  orders.Choice{Extras: order.Choice.Extras, Total: order.Choice.Total},
	}
}

func scriptToolKey(updateID int64, index, sequence int) string {
	return fmt.Sprintf("tg-script-%d-%d-%d", updateID, index, sequence)
}
