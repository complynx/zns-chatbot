package legacyfood

import (
	"context"
	"encoding/hex"
	"strings"
)

type Callback struct {
	Action  string  `json:"action"`
	EventID string  `json:"event_id"`
	OrderID string  `json:"order_id,omitempty"`
	Command Command `json:"command"`
}

func (s Service) ResolveCallback(ctx context.Context, actor, event, data string) (Callback, error) {
	parts := strings.Split(data, "|")
	if len(data) > 64 || len(parts) < 2 || parts[0] != "food" {
		return Callback{}, problem("invalid_callback")
	}
	result := Callback{Action: parts[1], EventID: event}
	withID := false
	switch result.Action {
	case "pay", "adm_acc", "adm_rej", "delete_order", callbackActivityPay, "adm_activities_acc", "adm_activities_rej":
		withID = true
	case "exit", callbackActivitySubmit, "activities_exit":
	case commandToggleActivity:
		if len(parts) != callbackIDParts {
			return result, problem("invalid_callback")
		}
		if _, err := ToggleActivities(Activities{}, parts[2], true); err != nil {
			return result, problem("invalid_callback")
		}
	default:
		return result, problem("invalid_callback")
	}
	order, err := s.callbackOrder(ctx, actor, parts, withID, &result)
	if err != nil {
		return result, err
	}
	result.Command = Command{EventID: result.EventID, OrderID: order.ID, Version: order.Version}
	if err = s.callbackCommand(ctx, actor, parts, &result, order); err != nil {
		return Callback{}, err
	}
	return result, nil
}

func (s Service) callbackOrder(
	ctx context.Context,
	actor string,
	parts []string,
	withID bool,
	result *Callback,
) (Order, error) {
	if withID {
		if len(parts) != callbackIDParts {
			return Order{}, problem("invalid_callback")
		}
		if raw, decodeErr := hex.DecodeString(parts[2]); decodeErr != nil || len(raw) != 12 {
			return Order{}, problem("invalid_callback")
		}
		order, err := s.resolveSourceOrder(ctx, actor, strings.ToLower(parts[2]))
		if err != nil {
			return Order{}, err
		}
		result.EventID, result.OrderID = order.EventID, order.ID
		return order, nil
	}
	if result.Action != commandToggleActivity && len(parts) != 2 {
		return Order{}, problem("invalid_callback")
	}
	var selected Event
	var err error
	if result.EventID == "" {
		selected, err = s.CurrentEvent(ctx, actor)
	} else {
		selected, err = s.Event(ctx, actor, result.EventID)
	}
	if err != nil {
		return Order{}, err
	}
	result.EventID = selected.ID
	if result.Action == commandToggleActivity || result.Action == callbackActivitySubmit {
		order, loadErr := loadOrder(ctx, s.DB, result.EventID, "", actor)
		if noOrder(loadErr) {
			return Order{}, nil
		}
		return order, loadErr
	}
	return Order{}, nil
}

func (s Service) callbackCommand(
	ctx context.Context,
	actor string,
	parts []string,
	result *Callback,
	order Order,
) error {
	command := &result.Command
	switch result.Action {
	case "pay", callbackActivityPay:
		if order.Owner != actor {
			return forbidden()
		}
		command.Name, command.Kind = "begin_payment", Meals
		if result.Action == callbackActivityPay {
			command.Kind = Activity
		}
	case "delete_order":
		if order.Owner != actor {
			return forbidden()
		}
		command.Name = commandDeleteMeals
	case commandToggleActivity:
		command.Name, command.Activity = commandToggleActivity, parts[2]
	case callbackActivitySubmit:
		if order.ID != "" && order.PaymentAdmin == "" {
			command.Name = "prepare_activities"
		}
	case "adm_acc", "adm_rej", "adm_activities_acc", "adm_activities_rej":
		return s.reviewCallback(ctx, actor, result, order)
	}
	return nil
}

func (s Service) reviewCallback(ctx context.Context, actor string, result *Callback, order Order) error {
	command := &result.Command
	if err := adminAllowed(ctx, s.DB, actor, order.EventID, "review"); err != nil {
		return err
	}
	command.Name, command.Kind = commandAccept, Meals
	if strings.HasSuffix(result.Action, "rej") {
		command.Name = commandReject
	}
	if strings.HasPrefix(result.Action, "adm_activities") {
		command.Kind = Activity
	}
	p, err := payment(&order, command.Kind)
	if err != nil {
		return err
	}
	if p.LegacySourceKey == "" || p.Generation != 0 || p.Status != Submitted {
		return problem("food_legacy_receipt_replaced")
	}
	command.Generation = p.Generation
	return nil
}
