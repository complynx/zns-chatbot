package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptInterrupted = "interrupted"
const scriptWorkflowView = "workflow"
const scriptWorkflowGet = "workflow.get"
const scriptWorkflowCatalog = "workflow.catalog"
const scriptOrdersList = "orders.list"
const scriptOrdersGet = "orders.get"
const scriptWorkflowSelect = "workflow.select"
const scriptOrdersChange = "orders.change"

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
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
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
		if !slices.ContainsFunc(input.Catalog, func(slot workflow.Slot) bool { return slot.ID == args.SlotID }) {
			return record, errors.New("unknown resource")
		}
		record.Action = &workflow.Action{
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
		command, err := (interaction.OrderCoordinator{Client: b.API, EventID: b.currentOrderEvent()}).Bind(
			ctx,
			owner,
			&proposal,
			input,
			agenthost.CurrentRequestEvidence(input),
		)
		record.Order = command
		return record, err
	default:
		return record, errors.New("tool unavailable")
	}
}

func (b *Bot) executeWorkflowOrderTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.Action != nil {
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		result, err := b.Host.ExecuteDerivedWorkflow(ctx, owner, *record.Action, *record.Source)
		if err == nil {
			input.Workflow = result
		}
		return result, err
	}
	if record.Order != nil {
		return b.executeScriptOrder(ctx, owner, record, input)
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
	input.Orders = interaction.OrderSummaries(
		list,
		agenthost.CurrentRequestEvidence(*input)+" "+selected,
		input.EditableOrderCount,
	)
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

// Definitive stale admission is a tool outcome, not a worker outage. The run
// remains redacted and the next model input is rebuilt from current sources.
func (b *Bot) executeScriptOrder(
	ctx context.Context,
	owner string,
	record agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("missing admitted source")
	}
	result, err := b.Host.ExecuteDerivedOrder(ctx, owner, *record.Order, *record.Source)
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
