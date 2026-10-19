package api

import "github.com/complynx/zns-chatbot/internal/orders"

func mealDocumentSchema() map[string]any {
	label := ordersObjectSchema(
		map[string]any{
			"en": map[string]any{manifestType: schemaString, schemaMinLength: 1, schemaMaxLength: orders.MaxLabelRunes},
			"ru": map[string]any{manifestType: schemaString, schemaMinLength: 1, schemaMaxLength: orders.MaxLabelRunes},
		},
		[]string{"en", "ru"},
	)
	price := mealInteger(0)
	price[schemaMaximum] = orders.MaxPrice
	item := ordersObjectSchema(
		map[string]any{
			"id":             mealIDSchema(),
			ordersSchemaName: label,
			"category": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"", "soup", "main", "side", "salad"},
			},
			schemaPriceMinor: price,
		},
		[]string{"id", ordersSchemaName, schemaPriceMinor},
	)
	serviceProperties := map[string]any{
		schemaItems: map[string]any{
			manifestType:         schemaArray,
			ordersSchemaMaxItems: orders.MaxItems,
			schemaItems:          item,
		},
	}
	for _, field := range []string{schemaStartsAt, schemaEndsAt, mealSchemaSalesOpens, detailsSalesCloses} {
		serviceProperties[field] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	}
	service := ordersObjectSchema(
		serviceProperties,
		[]string{schemaStartsAt, schemaEndsAt, mealSchemaSalesOpens, detailsSalesCloses, schemaItems},
	)
	combos := ordersObjectSchema(
		map[string]any{"without_soup_minor": price, "with_soup_minor": price},
		[]string{"without_soup_minor", "with_soup_minor"},
	)
	lunch := ordersObjectSchema(map[string]any{"service": service, "combos": combos}, []string{"service"})
	day := ordersObjectSchema(
		map[string]any{"id": mealIDSchema(), ordersSchemaName: label, "lunch": lunch, "dinner": service},
		[]string{"id", ordersSchemaName},
	)
	return ordersObjectSchema(
		map[string]any{
			schemaCurrency: map[string]any{manifestType: schemaString, schemaPattern: schemaCurrencyPattern},
			schemaCurrencyScale: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: orders.MaxCurrencyScale,
			},
			"days": map[string]any{
				manifestType:         schemaArray,
				ordersSchemaMinItems: 1,
				ordersSchemaMaxItems: orders.MaxMealDays,
				schemaItems:          day,
			},
		},
		[]string{schemaCurrency, schemaCurrencyScale, "days"},
	)
}
