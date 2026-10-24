package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestNotificationListStateSchemaPreservesValidation(t *testing.T) {
	t.Parallel()
	registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
	schema := huma.SchemaFromType(registry, reflect.TypeFor[notificationListArguments]())
	states := []any{
		"pending", "retryable", "send_inflight", "delivered",
		"delivery_unknown", "delivery_failed", "delivery_blocked", "dismissed",
	}
	require.Equal(t, states, schema.Properties["state"].Enum)
	for _, state := range append(states, "", "unknown", true, float64(1)) {
		result := &huma.ValidateResult{}
		huma.Validate(registry, schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer,
			map[string]any{"state": state}, result)
		valid := false
		for _, allowed := range states {
			valid = valid || state == allowed
		}
		if valid {
			require.Empty(t, result.Errors, "%v", state)
		} else {
			require.NotEmpty(t, result.Errors, "%v", state)
		}
	}
	result := &huma.ValidateResult{}
	huma.Validate(registry, schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, map[string]any{}, result)
	require.Empty(t, result.Errors, "state remains optional")
}

func TestSettingsSchemaDescriptionsDoNotChangeFieldContracts(t *testing.T) {
	t.Parallel()
	registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
	schema := registry.Schema(reflect.TypeFor[db.Settings](), true, "")
	schema = registry.SchemaFromRef(schema.Ref)
	before, err := json.Marshal(schema)
	require.NoError(t, err)
	describeSettingsSchema(registry)
	described := 0
	for _, property := range schema.Properties {
		if property.Description != "" {
			described++
		}
	}
	require.Equal(t, 17, described)
	for _, name := range []string{
		"sticker_catalog_limit", "emoji_catalog_limit", "symbol_half_life_seconds",
		"symbol_median_multiplier", "symbol_min_half_life_seconds", "symbol_max_half_life_seconds",
	} {
		require.NotEmpty(t, schema.Properties[name].Description, name)
	}
	require.Equal(t, "Worker concurrency; changes apply after application restart.",
		schema.Properties["max_parallel_tasks"].Description)
	require.Equal(t, "Telegram login proof maximum age; zero uses 300 seconds. Range 30 to 900.",
		schema.Properties["mini_app_max_age_seconds"].Description)
	for _, property := range schema.Properties {
		property.Description = ""
	}
	after, err := json.Marshal(schema)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

func TestNotificationResolutionKindSchemaPreservesValidation(t *testing.T) {
	t.Parallel()
	registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
	describeNotificationResolutionSchema(registry)
	schema := registry.Schema(reflect.TypeFor[db.NotificationResolution](), true, "")
	schema = registry.SchemaFromRef(schema.Ref)
	kinds := []any{"payment", "pair", "order", "massage", "massage_specialist", "knowledge"}
	require.Equal(t, kinds, schema.Properties["notification_kind"].Enum)
	for _, kind := range append(kinds, "", "unknown") {
		result := &huma.ValidateResult{}
		huma.Validate(registry, schema.Properties["notification_kind"], huma.NewPathBuffer(nil, 0),
			huma.ModeWriteToServer, kind, result)
		if kind == "" || kind == "unknown" {
			require.NotEmpty(t, result.Errors, "%v", kind)
		} else {
			require.Empty(t, result.Errors, "%v", kind)
		}
	}
}
