package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const OrdersView = "orders"

func validateOrderProposal(plan Plan) error {
	proposal := plan.OrderAction
	if proposal == nil {
		return nil
	}
	if plan.View != OrdersView || len(proposal.OrderID) > 64 || len(proposal.Extra) > 64 {
		return errors.New("invalid order proposal")
	}
	switch proposal.Name {
	case orderPaymentInstructions:
		if proposal.OrderID == "" || proposal.Extra != "" {
			return errors.New("invalid payment instructions proposal")
		}
	case assignmentCreateField, "export":
		if proposal.OrderID != "" || proposal.Extra != "" {
			return errors.New("invalid new order")
		}
	case orderAddExtra, orderRemoveExtra:
		if proposal.OrderID == "" || proposal.Extra == "" {
			return errors.New("missing order resource")
		}
	default:
		return errors.New("forbidden order proposal")
	}
	return nil
}

func wantsOrders(in Input) bool {
	text := strings.ToLower(in.Text)
	if wantsPaymentInstructions(text) {
		return true
	}
	if strings.Contains(text, "экспорт") || strings.Contains(text, "export") || strings.Contains(text, "xlsx") {
		return true
	}
	if strings.Contains(text, "заказ") || strings.Contains(text, "order") {
		return true
	}
	return in.View == OrdersView && !strings.Contains(text, "массаж") && !strings.Contains(text, "massage")
}

func scriptedOrderPlan(in Input) Plan {
	plan := Plan{
		View: OrdersView,
		Text: "Вижу текущие заказы и историю действий. Проверьте состав и итог в карточке; оплату выберите кнопкой.",
	}
	text := strings.ToLower(in.Text)
	if wantsPaymentInstructions(text) {
		return scriptedPaymentInstructions(in, plan)
	}
	if strings.Contains(text, "экспорт") || strings.Contains(text, "export") || strings.Contains(text, "xlsx") {
		plan.OrderAction = &OrderProposal{Name: "export"}
		return plan
	}
	if strings.Contains(text, "убрал") || strings.Contains(text, "removed") {
		removed := removedExtras(in.OrderHistory)
		plan.Text = "В доступной истории нет подтверждённого удаления услуги. Актуальный состав показан в карточке."
		if len(removed) > 0 {
			plan.Text = "Убрано из заказа: " + strings.Join(
				removed,
				", ",
			) + ". Актуальный состав показан в карточке."
		}
		return plan
	}
	if strings.Contains(text, "создай") || strings.Contains(text, assignmentCreateField) {
		plan.OrderAction = &OrderProposal{Name: assignmentCreateField}
		return plan
	}
	if !strings.Contains(text, "добав") && !strings.Contains(text, "убери") && !strings.Contains(text, "add") &&
		!strings.Contains(text, "remove") {
		return plan
	}
	extra := ""
	switch {
	case strings.Contains(text, "трансфер"), strings.Contains(text, "shuttle"):
		extra = "shuttle"
	case strings.Contains(text, "препати"), strings.Contains(text, "preparty"):
		extra = "preparty"
	}
	editable := editableOrders(in)
	if len(editable) != 1 || extra == "" {
		plan.Text = "Нужен один редактируемый заказ и однозначная услуга. Выберите нужные кнопки в карточке."
		return plan
	}
	name := orderAddExtra
	if strings.Contains(text, "убери") || strings.Contains(text, "remove") {
		name = orderRemoveExtra
	}
	plan.OrderAction = &OrderProposal{Name: name, OrderID: editable[0].ID, Extra: extra}
	return plan
}

func wantsPaymentInstructions(text string) bool {
	return strings.Contains(text, "реквизит") || strings.Contains(text, "как оплатить") ||
		strings.Contains(text, "payment instructions")
}

func scriptedPaymentInstructions(in Input, plan Plan) Plan {
	for _, order := range in.Orders {
		if in.OrderCount == 1 || strings.Contains(in.Text, order.ID) {
			plan.OrderAction = &OrderProposal{Name: orderPaymentInstructions, OrderID: order.ID}
			return plan
		}
	}
	plan.Text, _ = i18n.Translate(in.Language, i18n.PaymentSelectOrder, nil)
	return plan
}

func editableOrders(in Input) []OrderSummary {
	result := []OrderSummary{}
	for _, order := range in.Orders {
		if order.State != "unpaid" && order.State != "cash" {
			continue
		}
		if in.EditableOrderCount > 1 && !strings.Contains(in.Text, order.ID) {
			continue
		}
		result = append(result, order)
	}
	return result
}

func removedExtras(history []orders.Change) []string {
	for _, change := range slices.Backward(history) {
		if (change.Origin != "manual" && change.Origin != "agent") || change.Action != "edit" {
			continue
		}
		removed := []string{}
		for key := range change.BeforeExtras {
			if _, selected := change.Extras[key]; !selected && key != "total" {
				label := key
				switch key {
				case "shuttle":
					label = "Трансфер"
				case "preparty":
					label = "Препати"
				}
				removed = append(removed, label)
			}
		}
		for _, dish := range change.Dishes {
			if dish.Before > dish.After {
				removed = append(
					removed,
					fmt.Sprintf(
						"%s (%s/%s): %d порц.",
						dishName(dish.Name),
						dish.Day,
						dish.Meal,
						dish.Before-dish.After,
					),
				)
			}
		}
		slices.Sort(removed)
		return removed
	}
	return nil
}

func dishName(key string) string {
	var menu struct {
		Dishes map[string]struct {
			Name string `json:"name_ru"`
		} `json:"dishes"`
	}
	if json.Unmarshal(orders.MenuJSON, &menu) == nil && menu.Dishes[key].Name != "" {
		return menu.Dishes[key].Name
	}
	return key
}
