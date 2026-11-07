package api

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Keep the notification kind contract readable without long database tags.
func describeNotificationResolutionSchema(registry huma.Registry) {
	schema := registry.Schema(reflect.TypeFor[db.NotificationResolution](), true, "")
	if schema.Ref != "" {
		schema = registry.SchemaFromRef(schema.Ref)
	}
	schema.Properties["notification_kind"].Enum = []any{
		"payment", "pair", schemaOrder, "massage", "massage_specialist", "knowledge",
	}
	schema.PrecomputeMessages()
}
