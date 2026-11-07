package api

import "github.com/complynx/zns-chatbot/internal/orders"

const (
	ordersSchemaOfferID = "offer_id"
	ordersSchemaItemID  = "item_id"
)

func ordersCapacitySchema(label, id map[string]any) map[string]any {
	resource := ordersObjectSchema(map[string]any{
		"id":             id,
		ordersSchemaName: label,
		"ceiling": map[string]any{
			manifestType:  schemaInteger,
			schemaMinimum: 0,
			schemaMaximum: orders.MaxResourceUnits,
		},
	}, []string{"id", ordersSchemaName, "ceiling"})
	binding := ordersObjectSchema(map[string]any{
		ordersSchemaOfferID: id, ordersSchemaItemID: id, "resource_id": id,
		"units": map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: orders.MaxBindingUnits},
	}, []string{ordersSchemaOfferID, "resource_id", "units"})
	return ordersObjectSchema(map[string]any{
		schemaVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: 1},
		"resources": map[string]any{
			manifestType:         schemaArray,
			schemaItems:          resource,
			ordersSchemaMinItems: 0,
			ordersSchemaMaxItems: orders.MaxResources,
		},
		"bindings": map[string]any{
			manifestType:         schemaArray,
			schemaItems:          binding,
			ordersSchemaMinItems: 0,
			ordersSchemaMaxItems: orders.MaxCapacityBindings,
		},
	}, []string{schemaVersion, "resources", "bindings"})
}
