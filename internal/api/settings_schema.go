package api

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Keep settings descriptions in the API schema, outside database struct tags.
func describeSettingsSchema(registry huma.Registry) {
	schema := registry.Schema(reflect.TypeFor[db.Settings](), true, "")
	if schema.Ref != "" {
		schema = registry.SchemaFromRef(schema.Ref)
	}
	descriptions := map[string]string{
		"sticker_catalog_limit":        "Retained sticker descriptions per bot; zero uses 5000. Range 1 to 50000.",
		"emoji_catalog_limit":          "Retained custom emoji descriptions per bot; zero uses 5000. Range 1 to 50000.",
		"symbol_half_life_seconds":     "Fallback half-life with fewer than 10 reuse-interval samples; zero uses 30 days.",
		"symbol_min_half_life_seconds": "Minimum adaptive half-life; zero uses one hour.",
		"symbol_max_half_life_seconds": "Maximum adaptive half-life; zero uses 180 days.",
		"symbol_median_multiplier": "Multiplier of the per-catalog median reuse interval; " +
			"zero uses 1. Recomputed hourly.",
		"avatar_browser_storage_limit_bytes": "Global browser avatar original budget in bytes; " +
			"zero uses 10000000000 (10 GB). Range 1 to 10000000000. " +
			"Lowering stops admission without evicting unexpired originals.",
		"avatar_browser_retention_seconds": "Browser avatar source lifetime from upload; " +
			"zero uses 7776000 (90 days). Range 60 to 7776000. " +
			"Lowering also expires existing sources; completed JPEGs and receipts remain.",
		"massage_notify_client_prior_long_seconds": "Long massage reminder lead in nonnegative 32-bit seconds; " +
			"zero uses 3600. Changes reschedule pending/retryable reminders.",
		"massage_notify_client_prior_seconds": "Short massage reminder lead in nonnegative 32-bit seconds; " +
			"zero uses 600. Changes reschedule pending/retryable reminders.",
		"massage_max_bookings_per_window": "Ordinary-owner massage booking limit per configured party window; " +
			"zero uses the legacy default 3. Range 1 to 100.",
		settingsSchemaParallelTasks:   "Worker concurrency; changes apply after application restart.",
		settingsSchemaQuestionTimeout: "Reserved for future expectations. Does not change current Loom prompt expiry.",
		"name_expectation_seconds":    "Name request lifetime in seconds; zero uses 7200. Range 60 to 86400.",
		"mini_app_max_age_seconds":    "Telegram login proof maximum age; zero uses 300 seconds. Range 30 to 900.",
		"mini_app_pending_seconds":    "Login reconciliation window; zero uses 120 seconds. Range 30 to 300.",
		"mini_app_session_seconds": "Absolute browser session lifetime; zero uses 900 seconds. Range 60 to 3600. " +
			"Existing sessions do not extend.",
	}
	for name, description := range descriptions {
		schema.Properties[name].Description = description
	}
}
