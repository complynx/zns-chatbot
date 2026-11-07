package api

import (
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/complynx/zns-chatbot/internal/knowledge"
)

const (
	knowledgeSchemaPrefix     = "#/components/schemas/"
	knowledgeSchemaCollection = "collection"
	knowledgeSchemaQuery      = "query"
	knowledgeQueryLimit       = 3
)

// Inline local type schemas because Loom discovery has no shared OpenAPI registry.
func knowledgeSchema[T any]() map[string]any {
	registry := huma.NewMapRegistry(knowledgeSchemaPrefix, huma.DefaultSchemaNamer)
	schema := huma.SchemaFromType(registry, reflect.TypeFor[T]())
	expanded := knowledgeExpandSchema(knowledgeSchemaJSON(schema), registry)
	result, ok := expanded.(map[string]any)
	if !ok {
		return nil
	}
	return result
}

func knowledgeSchemaJSON(value any) any {
	encoded, _ := json.Marshal(value)
	var result any
	_ = json.Unmarshal(encoded, &result)
	return result
}

func knowledgeExpandSchema(value any, registry huma.Registry) any {
	switch v := value.(type) {
	case map[string]any:
		if types, ok := v[manifestType].([]any); ok {
			return knowledgeNullableSchema(v, types, registry)
		}
		if ref, ok := v["$ref"].(string); ok {
			target := registry.Map()[strings.TrimPrefix(ref, knowledgeSchemaPrefix)]
			return knowledgeExpandSchema(knowledgeSchemaJSON(target), registry)
		}
		for key, item := range v {
			v[key] = knowledgeExpandSchema(item, registry)
		}
	case []any:
		for index, item := range v {
			v[index] = knowledgeExpandSchema(item, registry)
		}
	}
	return value
}

// Consumers decode single type names. anyOf preserves nullable type arrays.
func knowledgeNullableSchema(fields map[string]any, types []any, registry huma.Registry) map[string]any {
	alternatives := make([]any, 0, len(types))
	for _, typ := range types {
		branch := map[string]any{manifestType: typ}
		if typ != miniAppPassPaymentNull {
			branch = maps.Clone(fields)
			branch[manifestType] = typ
		}
		alternatives = append(alternatives, knowledgeExpandSchema(branch, registry))
	}
	return map[string]any{mealSchemaAnyOf: alternatives}
}

func knowledgeInputSchema[T any]() map[string]any {
	schema := knowledgeSchema[T]()
	knowledgeConstrainSchema(schema)
	if reflect.TypeFor[T]() == reflect.TypeFor[knowledgeOutlineArguments]() {
		properties, ok := schema[schemaProperties].(map[string]any)
		if ok {
			knowledgeSetFieldBounds(properties, schemaLimit, map[string]any{schemaMaximum: knowledge.MaxOutlineLimit})
		}
	}
	return schema
}

func knowledgeApplyBounds(field, values map[string]any) {
	choices, ok := field[mealSchemaAnyOf].([]any)
	if !ok {
		maps.Copy(field, values)
		return
	}
	for _, choice := range choices {
		object, isObject := choice.(map[string]any)
		if isObject && object[manifestType] != miniAppPassPaymentNull {
			knowledgeApplyBounds(object, values)
		}
	}
}

func knowledgeSetFieldBounds(properties map[string]any, name string, values map[string]any) {
	field, ok := properties[name].(map[string]any)
	if ok {
		knowledgeApplyBounds(field, values)
	}
}

func knowledgeConstrainSchema(value any) {
	switch v := value.(type) {
	case map[string]any:
		if properties, ok := v[schemaProperties].(map[string]any); ok {
			for name, bounds := range knowledgeFieldBounds() {
				knowledgeSetFieldBounds(properties, name, bounds)
			}
		}
		for _, item := range v {
			knowledgeConstrainSchema(item)
		}
	case []any:
		for _, item := range v {
			knowledgeConstrainSchema(item)
		}
	}
}

func knowledgeFieldBounds() map[string]map[string]any {
	return map[string]map[string]any{
		knowledgeSchemaCollection: {
			schemaEnum: []string{string(knowledge.All), string(knowledge.General), string(knowledge.Event)},
		},
		"language": {schemaEnum: []string{"en", "ru"}},
		"queries": {
			ordersSchemaMinItems: 1,
			ordersSchemaMaxItems: knowledgeQueryLimit,
			schemaItems: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: knowledge.MaxQueryRunes,
			},
		},
		knowledgeSchemaQuery: {schemaMinLength: 1, schemaMaxLength: knowledge.MaxQueryRunes},
		"cursor":             {schemaMaxLength: knowledge.MaxCursorBytes},
		"topic":              {schemaMaxLength: knowledge.MaxTopicRunes},
		"slug":               {schemaMinLength: 1, schemaMaxLength: knowledge.MaxSlugRunes},
		"title":              {schemaMinLength: 1, schemaMaxLength: knowledge.MaxTitleRunes},
		"label":              {schemaMinLength: 1, schemaMaxLength: knowledge.MaxTitleRunes},
		"url":                {schemaMinLength: 1, schemaMaxLength: knowledge.MaxSourceURLBytes},
		"body":               {schemaMinLength: 1, schemaMaxLength: knowledge.MaxBodyBytes},
		"topics": {
			ordersSchemaMaxItems: knowledge.MaxTopics,
			schemaItems: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: knowledge.MaxTopicRunes,
			},
		},
		"sources":       {ordersSchemaMaxItems: knowledge.MaxSources},
		"start_index":   {schemaMinimum: 0},
		"start_byte":    {schemaMinimum: 0},
		"start_line":    {schemaMinimum: 1},
		"line_count":    {schemaMinimum: 1, schemaMaximum: knowledge.MaxReadLines},
		"max_bytes":     {schemaMinimum: utf8.UTFMax, schemaMaximum: knowledge.MaxReadBytes},
		"context_bytes": {schemaMinimum: 0, schemaMaximum: knowledge.MaxContextBytes},
		schemaLimit:     {schemaMinimum: 1, schemaMaximum: knowledge.MaxLimit},
	}
}
