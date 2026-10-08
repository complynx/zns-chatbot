package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

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

const modernOrderEdit = "edit"

type modernOrderArguments struct {
	ChoiceRef string              `json:"choice_ref,omitempty"`
	Resume    bool                `json:"resume,omitempty"`
	Event     string              `json:"event,omitempty"`
	OrderID   string              `json:"order_id,omitempty"`
	Entry     string              `json:"entry,omitempty"`
	Cursor    string              `json:"cursor,omitempty"`
	Name      string              `json:"name,omitempty"`
	Country   string              `json:"country,omitempty"`
	Contact   string              `json:"contact,omitempty"`
	Choice    *orders.ChoiceInput `json:"choice,omitempty"`
}

type modernOrderSummary struct {
	ID      string       `json:"id"`
	EventID string       `json:"event_id"`
	Owner   string       `json:"owner,omitempty"`
	Version int64        `json:"version"`
	State   string       `json:"state"`
	Total   orders.Money `json:"total"`
}

func modernSummary(order orders.Order) modernOrderSummary {
	return modernOrderSummary{
		ID:      order.ID,
		EventID: order.EventID,
		Owner:   order.Owner,
		Version: order.Version,
		State:   order.State,
		Total:   order.Choice.Total,
	}
}

func (b *Bot) scriptModernOrderEntries(capability core.BusinessCapabilities) []agenthost.ScriptToolEntry {
	return (agenthost.OrderScriptCatalog{
		OrderBinding: agenthost.ScriptToolEntry{
			Prepare: b.prepareModernOrderTool, Execute: b.executeModernOrderTool, ResultLimit: maxScriptReadBytes,
		},
		ChoiceBinding: agenthost.ScriptToolEntry{
			Prepare: b.prepareModernChoice, Execute: b.executeModernChoice, ResultLimit: maxScriptReadBytes,
		},
	}).Entries(capability)
}

func (b *Bot) prepareModernOrderTool(
	ctx context.Context,
	owner string,
	update int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var args modernOrderArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	if !validModernOrderArguments(call.Name, args) {
		return record, errors.New("invalid modern order arguments")
	}
	event := args.Event
	if event == "" {
		event = b.currentOrderEvent()
	}
	if err := b.prepareModernOrderChoice(ctx, owner, call.Name, &args, &record); err != nil {
		return record, err
	}
	if args.Event != "" {
		event = args.Event
	}
	record.ModernOrder = &agenthost.ModernOrderRequest{Event: event, Update: update}
	if call.Name == modernOrdersInspect || call.Name == modernOrdersReviewRead {
		record.ModernOrder.ReadOrderID = args.OrderID
		if err := b.prepareModernOrderRead(ctx, owner, call.Name, args, record.ModernOrder); err != nil {
			return record, err
		}
	}
	if call.Name == modernOrdersUpdate && args.Name == actionCreateOrder {
		record.Order = &orders.Command{
			EventID: event,
			Name:    actionCreateOrder,
			Choice:  args.Choice,
			Origin:  originAgent,
		}
		return record, nil
	}
	if call.Name == modernOrdersUpdate || call.Name == modernOrdersReviewDecide ||
		call.Name == modernOrdersInstructions ||
		call.Name == modernOrdersProof ||
		call.Name == modernOrdersReviewProof {
		command, err := b.bindModernOrder(ctx, owner, call.Name, args, input)
		if err != nil {
			return record, err
		}
		record.Order = command
		record.ModernOrder.Event = command.EventID
	}
	if call.Name == modernOrdersExport || call.Name == modernOrdersProof || call.Name == modernOrdersReviewProof {
		source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
		if !ok || source.owner != owner {
			return record, errors.New("order delivery context missing")
		}
		record.ModernOrder.Chat = source.in.chat
	}
	return record, nil
}

func (b *Bot) bindModernOrder(
	ctx context.Context,
	owner, name string,
	args modernOrderArguments,
	input agent.Input,
) (*orders.Command, error) {
	observed := input.ModernOrder
	review := strings.HasPrefix(name, "orders.review.")
	if observed == nil || input.ModernOrderCursor != "" || input.ModernOrderReview != review ||
		observed.ID != args.OrderID ||
		(args.Event != "" && args.Event != observed.EventID) {
		return nil, errors.New("complete matching order read required")
	}
	if review && observed.EventID != b.currentOrderEvent() {
		return nil, errors.New("review event changed")
	}
	if !review && name == modernOrdersUpdate {
		list, err := b.API.Orders(ctx, owner, observed.EventID)
		if err != nil {
			return nil, err
		}
		if len(list) > 1 && !strings.Contains(agenthost.CurrentRequestEvidence(input), args.OrderID) {
			return nil, errors.New("explicit order selection required")
		}
	}
	return &orders.Command{
		EventID:      observed.EventID,
		OrderID:      observed.ID,
		Version:      observed.Version,
		Attempt:      observed.Attempt,
		Name:         args.Name,
		Choice:       args.Choice,
		Country:      args.Country,
		PaymentAdmin: args.Contact,
		Origin:       originAgent,
	}, nil
}

func validModernOrderArguments(name string, args modernOrderArguments) bool {
	if args.Resume && ((name != modernOrdersInspect && name != modernOrdersReviewRead) || args.Cursor != "") {
		return false
	}
	if !validModernChoiceReference(name, args) {
		return false
	}
	if len(args.Event) > 128 || len(args.OrderID) > 128 || len(args.Entry) > 64 || len(args.Contact) > 128 ||
		len(args.Cursor) > 2048 {
		return false
	}
	switch name {
	case modernOrdersQuote:
		return (args.Choice != nil || args.ChoiceRef != "") && args.OrderID == "" && args.Entry == "" &&
			args.Name == "" &&
			args.Country == "" &&
			args.Contact == ""
	case modernOrdersReviewDecide:
		return (args.Name == passAccept || args.Name == knowledgeRejectDecision) && args.OrderID != "" &&
			args.Event == "" &&
			args.Entry == "" &&
			args.Cursor == "" &&
			args.Choice == nil &&
			args.Country == "" &&
			args.Contact == ""
	case modernOrdersUpdate:
		return validModernOrderUpdate(args)
	}
	return validModernOrderRead(name, args)
}

func validModernOrderRead(name string, args modernOrderArguments) bool {
	if args.Name != "" || args.Country != "" || args.Contact != "" || args.Choice != nil {
		return false
	}
	switch name {
	case modernOrdersEvents, modernOrdersInbox:
		return args.Event == "" && args.OrderID == "" && args.Entry == ""
	case modernOrdersEvent, modernOrdersContacts, modernOrdersHistory, modernOrdersBrowse:
		return args.OrderID == "" && args.Entry == ""
	case modernOrdersHistoryRead:
		return args.OrderID == "" && args.Entry != ""
	case modernOrdersInspect:
		return args.OrderID != "" && args.Entry == ""
	case modernOrdersReviewRead, modernOrdersInstructions:
		return args.Event == "" && args.OrderID != "" && args.Entry == ""
	case modernOrdersProof, modernOrdersReviewProof:
		return args.Event == "" && args.OrderID != "" && args.Entry == "" && args.Cursor == ""
	case modernOrdersExport:
		return args.Event == "" && args.OrderID == "" && args.Entry == "" && args.Cursor == ""
	}
	return false
}

func validModernOrderUpdate(args modernOrderArguments) bool {
	if args.Entry != "" || args.Cursor != "" {
		return false
	}
	switch args.Name {
	case actionCreateOrder:
		return args.OrderID == "" && (args.Choice != nil || args.ChoiceRef != "") && args.Country == "" &&
			args.Contact == ""
	case modernOrderEdit:
		return args.OrderID != "" && (args.Choice != nil || args.ChoiceRef != "") && args.Country == "" &&
			args.Contact == ""
	case "delete", "cancel_proof":
		return args.OrderID != "" && args.Choice == nil && args.Country == "" && args.Contact == ""
	case "cash":
		return args.OrderID != "" && args.Choice == nil && args.Country == "" && args.Contact != ""
	case "country":
		return args.OrderID != "" && args.Choice == nil && (args.Country == "ru" || args.Country == "be") &&
			args.Contact != ""
	}
	return false
}

func (b *Bot) executeModernOrderTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.ModernOrder == nil {
		return nil, errors.New("modern order binding missing")
	}
	var args modernOrderArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	event := record.ModernOrder.Event
	switch call.Name {
	case modernOrdersExport, modernOrdersProof, modernOrdersReviewProof:
		return b.deliverModernOrder(ctx, owner, call.Name, record)
	case modernOrdersUpdate, modernOrdersReviewDecide:
		if record.Order == nil {
			return nil, errors.New("order command missing")
		}
		if record.ChoiceUse != "" && record.Order.HistoryGeneration == nil {
			return nil, appclient.ErrReadStale
		}
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		order, err := b.Host.ExecuteDerivedOrder(ctx, owner, *record.Order, *record.Source)
		if err != nil {
			return nil, err
		}
		input.ModernOrder = nil
		return modernSummary(order), nil
	case modernOrdersInspect, modernOrdersReviewRead:
		return b.readModernOrder(ctx, owner, call.Name, event, args, record.ModernOrder, input)
	case modernOrdersEvents:
		result, err := b.API.OrderEventsPage(ctx, owner, args.Cursor)
		return result, appclient.ReadError(err)
	case modernOrdersHistory:
		result, err := b.API.OrderHistoryPage(ctx, owner, event, args.Cursor)
		return result, appclient.ReadError(err)
	case modernOrdersHistoryRead:
		result, err := b.API.OrderHistoryDetail(ctx, owner, event, args.Entry, args.Cursor)
		return result, appclient.ReadError(err)
	case modernOrdersInbox, modernOrdersBrowse:
		return b.modernOrderPage(ctx, owner, event, args.Cursor, call.Name)
	default:
		return b.modernOrderChunk(ctx, owner, call.Name, event, args, record)
	}
}

func (b *Bot) modernOrderChunk(
	ctx context.Context,
	owner, name, event string,
	args modernOrderArguments,
	record agenthost.ScriptToolRecord,
) (any, error) {
	if name == modernOrdersEvent {
		value, err := b.API.OrderEventChunk(ctx, owner, event, args.Cursor, false)
		return value, appclient.ReadError(err)
	}
	scopeArgs := args
	scopeArgs.Cursor = ""
	scope, _ := json.Marshal(scopeArgs)
	digest := sha256.Sum256(scope)
	cursor, err := core.DecodeReadCursor(args.Cursor, owner, name+":"+event+":"+hex.EncodeToString(digest[:]))
	if err != nil {
		return nil, appclient.ReadError(err)
	}
	var value any
	switch name {
	case modernOrdersContacts:
		value, err = b.API.PaymentAdmins(ctx, owner, event)
	case modernOrdersQuote:
		if args.ChoiceRef != "" {
			if args.Event != "" && args.Event != event {
				return nil, errors.New("choice event mismatch")
			}
			value, err = b.currentModernQuote(ctx, owner, event, args.ChoiceRef)
		} else {
			value, err = b.API.QuoteOrder(ctx, owner, event, *args.Choice)
		}
	case modernOrdersInstructions:
		current, readErr := b.API.Order(ctx, owner, event, args.OrderID)
		if readErr != nil {
			return nil, readErr
		}
		if record.Order == nil || current.Version != record.Order.Version || current.Attempt != record.Order.Attempt {
			return nil, appclient.ErrReadStale
		}
		info, instructionErr := b.API.PaymentInstructions(ctx, owner, event, args.OrderID)
		if instructionErr == nil && info.Version != record.Order.Version {
			return nil, appclient.ErrReadStale
		}
		value, err = info, instructionErr
	default:
		return nil, errors.New("unknown modern order tool")
	}
	if err != nil {
		return nil, err
	}
	result, err := core.JSONReadChunk(value, cursor)
	return result, appclient.ReadError(err)
}

func (b *Bot) readModernOrder(
	ctx context.Context,
	owner, name, event string,
	args modernOrderArguments,
	request *agenthost.ModernOrderRequest,
	input *agent.Input,
) (any, error) {
	review := name == modernOrdersReviewRead
	input.ModernOrder = nil
	input.ModernOrderCursor = ""
	if args.OrderID != request.ReadOrderID || (!args.Resume && args.Cursor != request.ReadCursor) {
		return nil, appclient.ErrReadStale
	}
	order, err := b.modernOrderReadSource(ctx, owner, name, event, request.ReadOrderID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := modernOrderFingerprint(order)
	if err != nil {
		return nil, err
	}
	if request.ReadSnapshot == "" || request.ReadSnapshot != fingerprint {
		return nil, appclient.ErrReadStale
	}
	cursor, err := core.DecodeReadCursor(request.ReadCursor, owner, name+":"+event+":"+request.ReadOrderID)
	if err != nil {
		return nil, appclient.ReadError(err)
	}
	safe := order
	safe.ProofFile = ""
	safe.Attempt = ""
	result, err := core.JSONReadChunk(safe, cursor)
	if err != nil {
		return nil, appclient.ReadError(err)
	}
	input.ModernOrder = &order
	input.ModernOrderReview = review
	input.ModernOrderCursor = result.NextCursor
	return modernOrderReadChunk{ReadChunk: result, Offset: cursor.Offset}, nil
}

// Bind the read checkpoint before admission. Execution rechecks the source and
// leaves these request fields unchanged for the ledger's completion comparison.
func (b *Bot) prepareModernOrderRead(
	ctx context.Context, owner, name string, args modernOrderArguments, request *agenthost.ModernOrderRequest,
) error {
	event := request.Event
	previous, err := b.modernOrderReadCheckpoint(ctx, owner, name, event, args)
	if err != nil {
		return err
	}
	if args.Resume {
		args.Cursor = previous.ReadCursor
	}
	fingerprint := previous.ReadSnapshot
	// A durable continuation already binds the snapshot. Execution reloads and
	// checks the current authorized order before returning its next page.
	if fingerprint == "" {
		order, readErr := b.modernOrderReadSource(ctx, owner, name, event, args.OrderID)
		if readErr != nil {
			return readErr
		}
		fingerprint, err = modernOrderFingerprint(order)
		if err != nil {
			return err
		}
	}
	_, err = core.DecodeReadCursor(args.Cursor, owner, name+":"+event+":"+args.OrderID)
	if err != nil {
		return appclient.ReadError(err)
	}
	request.ReadCursor = args.Cursor
	request.ReadSnapshot = fingerprint
	return nil
}

func (b *Bot) modernOrderReadSource(ctx context.Context, owner, name, event, id string) (orders.Order, error) {
	if name == modernOrdersReviewRead {
		return b.API.ReviewOrder(ctx, owner, event, id)
	}
	return b.API.Order(ctx, owner, event, id)
}

func (b *Bot) modernOrderPage(ctx context.Context, owner, event, raw, name string) (any, error) {
	cursor, err := readScriptCursor(raw, owner, name, event)
	if err != nil {
		return nil, err
	}
	page, err := b.API.OrdersPage(ctx, owner, event, cursor.Position, name == modernOrdersInbox)
	if err != nil {
		return nil, err
	}
	items := make([]modernOrderSummary, 0, len(page.Orders))
	for _, order := range page.Orders {
		items = append(items, modernSummary(order))
	}
	next := ""
	if page.Next != "" {
		cursor.Position = page.Next
		next = encodeScriptCursor(cursor)
	}
	return core.ReadPage[modernOrderSummary]{Items: items, NextCursor: next, More: next != ""}, nil
}

func validModernChoiceReference(name string, args modernOrderArguments) bool {
	if args.ChoiceRef == "" {
		return true
	}
	return len(args.ChoiceRef) <= 96 && args.Choice == nil && (name == modernOrdersQuote || name == modernOrdersUpdate)
}
