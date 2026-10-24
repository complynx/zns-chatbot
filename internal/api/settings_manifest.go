package api

import "github.com/complynx/zns-chatbot/internal/db"

const (
	settingsSchemaNumber             = "number"
	settingsMinMedianMultiplier      = 0.01
	settingsMaxMedianMultiplier      = 1000
	settingsSchemaMaxMassageBookings = 100
	settingsSchemaMaxWorkers         = 128
	settingsSchemaMaxQuestionSeconds = 604800
	settingsSchemaMinNameSeconds     = 60
	settingsSchemaMaxNameSeconds     = 86400
	settingsSchemaMinLoginSeconds    = 30
	settingsSchemaMaxLoginSeconds    = 900
	settingsSchemaMinSessionSeconds  = 60
	settingsSchemaMaxSessionSeconds  = 3600
	settingsSchemaMaxSymbolEntries   = 50000
	settingsSchemaMinSymbolHalfLife  = 60
	settingsSchemaMaxSymbolHalfLife  = 365 * 24 * 60 * 60
)

func settingsIntegerSchema(minimum, maximum int) map[string]any {
	return map[string]any{manifestType: schemaInteger, schemaMinimum: minimum, schemaMaximum: maximum}
}

func optionalSettingsIntegerSchema(minimum, maximum int) map[string]any {
	return map[string]any{manifestType: schemaInteger, "anyOf": []any{
		map[string]any{schemaEnum: []int{0}}, settingsIntegerSchema(minimum, maximum),
	}}
}

func settingsDocumentSchema() map[string]any {
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			"auth_cache_ttl_seconds",
			"default_language",
			settingsSchemaParallelTasks,
			settingsSchemaQuestionTimeout,
		},
		schemaProperties: map[string]any{
			"sticker_catalog_limit": optionalSettingsIntegerSchema(1, settingsSchemaMaxSymbolEntries),
			"emoji_catalog_limit":   optionalSettingsIntegerSchema(1, settingsSchemaMaxSymbolEntries),
			"symbol_half_life_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinSymbolHalfLife,
				settingsSchemaMaxSymbolHalfLife,
			),
			"symbol_min_half_life_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinSymbolHalfLife,
				settingsSchemaMaxSymbolHalfLife,
			),
			"symbol_max_half_life_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinSymbolHalfLife,
				settingsSchemaMaxSymbolHalfLife,
			),
			"symbol_median_multiplier": map[string]any{manifestType: settingsSchemaNumber, mealSchemaAnyOf: []any{
				map[string]any{schemaEnum: []int{0}}, map[string]any{
					schemaMinimum: settingsMinMedianMultiplier, schemaMaximum: settingsMaxMedianMultiplier,
				},
			}},
			"avatar_browser_storage_limit_bytes": optionalSettingsIntegerSchema(
				1,
				int(db.MaxAvatarBrowserStorageBytes),
			),
			"avatar_browser_retention_seconds": optionalSettingsIntegerSchema(
				db.MinAvatarBrowserRetentionSeconds,
				db.MaxAvatarBrowserRetentionSeconds,
			),
			"auth_cache_ttl_seconds": settingsIntegerSchema(0, settingsMaxCacheTTLSeconds),
			"default_language": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"en", "ru"},
			},
			settingsSchemaParallelTasks:   settingsIntegerSchema(1, settingsSchemaMaxWorkers),
			settingsSchemaQuestionTimeout: settingsIntegerSchema(1, settingsSchemaMaxQuestionSeconds),
			"massage_max_bookings_per_window": optionalSettingsIntegerSchema(
				1,
				settingsSchemaMaxMassageBookings,
			),
			"massage_notify_client_prior_long_seconds": optionalSettingsIntegerSchema(
				1,
				db.MaxMassageReminderLeadSeconds,
			),
			"massage_notify_client_prior_seconds": optionalSettingsIntegerSchema(
				1,
				db.MaxMassageReminderLeadSeconds,
			),
			"name_expectation_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinNameSeconds,
				settingsSchemaMaxNameSeconds,
			),
			"mini_app_max_age_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinLoginSeconds,
				settingsSchemaMaxLoginSeconds,
			),
			"mini_app_pending_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinLoginSeconds,
				settingsMaxCacheTTLSeconds,
			),
			"mini_app_session_seconds": optionalSettingsIntegerSchema(
				settingsSchemaMinSessionSeconds,
				settingsSchemaMaxSessionSeconds,
			),
		},
	}
}

func settingsChangeMethod() map[string]any {
	method := administrativeRead(
		"update_settings",
		"Replace application-wide runtime settings after reading settings and obtaining exact "+
			"approval. Supply expected_version and the full document; "+
			"omitted optional zero values retain documented defaults. "+
			"Massage reminder lead changes reschedule pending/retryable reminders without clearing "+
			"retry delays; delivered, unknown and inflight remain unchanged. "+
			"Browser source retention applies to existing uploads; "+
			"expired bytes are irrecoverable, completed JPEGs and receipts remain. "+
			"Lowering the byte quota blocks new uploads without quota-driven eviction. "+
			"Worker concurrency applies after restart, question timeout is reserved. "+
			"No startup config or secrets are editable. "+
			"An original receipt is historical: current process IAM TTL is synchronized from CURRENT "+
			"DB settings, never from a replayed document. "+
			"This does not coordinate other running processes. "+
			"Reconcile the same operation after any lost response/503; "+
			"not_found never proves a safe replacement write.",
		map[string]any{
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			schemaDocument:        settingsDocumentSchema(),
		},
		[]string{schemaExpectedVersion, schemaDocument},
		[]string{schemaVersion, schemaDocument},
	)
	method[manifestEffect] = effectWrite
	method[manifestIdempotency] = true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = settingsChangePath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: settingsChangePath + "/prepare",
	}
	return method
}
