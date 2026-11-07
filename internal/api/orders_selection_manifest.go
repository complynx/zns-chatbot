package api

import "github.com/complynx/zns-chatbot/internal/orders"

const ordersSchemaSelections = "selections"
const ordersSchemaEventVersion = "event_version"

func ordersSelectionMethods() []map[string]any {
	fields := []string{
		schemaEventID,
		ordersSchemaEventVersion,
		"catalog_version",
		schemaVersion,
		ordersSchemaSelections,
	}
	read := paymentMethod(
		"selection",
		"Read your saved event selection and historical quote. Version zero means no saved draft. "+
			"Current assigned pass and enabled orders module are required. "+
			"Closed or removed catalog items remain in saved history; "+
			"never imply a reservation, submitted order or current price guarantee.",
		ordersEventProperties(),
		[]string{schemaEventID},
		fields,
	)
	read[manifestPath] = ordersSelectionPath + "selection"
	properties := ordersEventProperties()
	properties[schemaExpectedEventVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	properties["expected_catalog_version"] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	properties["expected_draft_version"] = map[string]any{manifestType: schemaInteger, schemaMinimum: 0}
	properties[ordersSchemaSelections] = ordersSelectionSchema(0)
	save := paymentMethod(
		"save_selection",
		"Save only quantities explicitly requested by the user as an unsubmitted draft. "+
			"Read catalog and selection first; supply their event, catalog and draft versions. "+
			"The server calculates and stores the complete historical quote; "+
			"no client price is accepted. Explicit selections:[] clears only this draft and advances "+
			"its revision, with no quote or order cancellation. Missing/null selections is invalid. "+
			"Stale versions conflict; do not silently reprice or overwrite. "+
			"No recipient, capacity hold, debt, payment or submitted order is created. "+
			"Same operation and normalized body replay the original receipt even after newer changes; "+
			"changed arguments require a new operation only after user intent. "+
			"On uncertain response reconcile the same operation; "+
			"not_found is unknown, not permission to replace it.",
		properties,
		[]string{
			schemaEventID,
			schemaExpectedEventVersion,
			"expected_catalog_version",
			"expected_draft_version",
			ordersSchemaSelections,
		},
		fields,
	)
	save[manifestPath] = ordersSelectionPath + "save_selection"
	save[manifestEffect], save[manifestIdempotency] = effectWrite, true
	save[manifestStatusPath] = ordersSelectionPath + "save_selection/status"
	save[manifestDedupWindow] = agentRegistrationDedupSeconds
	operation := paymentMethod(
		"selection_operation",
		"Recover your original saved-selection receipt by operation UUID. "+
			"This returns historical quantities/quote only after checking current access to that "+
			"operation's event. not_found is an unknown result, never evidence that a retry with a new "+
			"operation is safe.",
		map[string]any{detailsOperationID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}},
		[]string{detailsOperationID},
		fields,
	)
	operation[manifestPath] = ordersSelectionPath + "selection_operation"
	list := paymentMethod(
		"events",
		"List events where your assigned pass permits orders, including unpaid/overdue and past "+
			"events needed for saved selection history. UUID pagination returns at most20 events; "+
			"use next as after until absent. Disabled orders modules, other owners and waitlist alone "+
			"grant no access. Dates use the supplied IANA timezone. "+
			"Listing never selects an event or changes a draft.",
		map[string]any{schemaAfter: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}},
		nil,
		[]string{schemaEvents},
	)
	list[manifestPath] = ordersSelectionPath + "events"
	return []map[string]any{list, read, save, operation}
}

func ordersSelectionSchema(minItems int) map[string]any {
	return map[string]any{
		manifestType:         schemaArray,
		ordersSchemaMinItems: minItems,
		ordersSchemaMaxItems: orders.MaxSelections,
		schemaItems: ordersObjectSchema(map[string]any{
			"offer_id": map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern},
			"item_id":  map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern},
			"quantity": map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
				schemaMaximum: orders.MaxQuantity,
			},
		}, []string{"offer_id", "item_id", "quantity"}),
	}
}
