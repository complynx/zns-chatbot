package agent

import (
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// BusinessCapabilities is current host evidence for the active order event.
// Missing evidence exposes only ordinary reads, never mutation or export tools.
type BusinessCapabilities = core.BusinessCapabilities

func canBook(input Input) bool {
	return input.Business != nil && input.Business.CanBook
}

func canExportOrders(input Input) bool {
	return canBook(input) && input.Business.CanExportOrders
}

const orderPaymentInstructions = orders.ActionPaymentInstructions
const orderAddExtra = "add_extra"
const orderRemoveExtra = "remove_extra"

func visibleOrderNames(input Input) []string {
	if !canBook(input) {
		return nil
	}
	names := []string{orderPaymentInstructions, "create", orderAddExtra, orderRemoveExtra}
	if canExportOrders(input) {
		names = append(names, "export")
	}
	return names
}
