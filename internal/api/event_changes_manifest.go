package api

import (
	"math"

	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const (
	eventSchemaMaxTiers         = 100
	eventSchemaLeaderCapacity   = "leader_capacity"
	eventSchemaFollowerCapacity = "follower_capacity"
	eventSchemaClosesAt         = "closes_at"
	eventSchemaPaymentWindow    = "payment_window_seconds"
	eventSchemaModules          = "modules"
	eventSchemaUniqueItems      = "uniqueItems"
)

func nullableEventPolicy(policy map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{policy, map[string]any{manifestType: "null"}}}
}

func eventTierPolicySchema() map[string]any {
	return map[string]any{
		manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{confirmationMode, "tiers"},
		schemaProperties: map[string]any{
			confirmationMode: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{registrations.PricingPaired, registrations.PricingDistributed},
			},
			"tiers": map[string]any{
				manifestType: schemaArray, "minItems": 1, ordersSchemaMaxItems: eventSchemaMaxTiers,
				schemaItems: map[string]any{
					manifestType: schemaObject, schemaAdditionalProperties: false,
					schemaRequired: []string{"id", "amount", schemaPriceMinor, schemaStartsAt},
					schemaProperties: map[string]any{
						"id": map[string]any{
							manifestType:  schemaString,
							schemaPattern: "^[a-z0-9][a-z0-9_-]{0,63}$",
						},
						"amount": map[string]any{
							manifestType:  schemaInteger,
							schemaMinimum: 0,
							schemaMaximum: math.MaxInt32,
						},
						schemaPriceMinor: map[string]any{
							manifestType:  schemaInteger,
							schemaMinimum: 1,
							schemaMaximum: agentJSONIntegerMax,
						},
						schemaStartsAt:    map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
						"promo":           map[string]any{manifestType: schemaBoolean},
						"blocked_by_date": map[string]any{manifestType: schemaBoolean},
					},
				},
			},
		},
	}
}

func eventRegistrationRulesSchema() map[string]any {
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			eventSchemaLeaderCapacity,
			eventSchemaFollowerCapacity,
			schemaPriceMinor,
			schemaCurrency,
			schemaCurrencyScale,
			eventSchemaClosesAt,
			eventSchemaPaymentWindow,
		},
		schemaProperties: map[string]any{
			eventSchemaLeaderCapacity: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: math.MaxInt32,
			},
			eventSchemaFollowerCapacity: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: math.MaxInt32,
			},
			schemaPriceMinor: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: agentJSONIntegerMax,
			},
			schemaCurrency: map[string]any{manifestType: schemaString, schemaPattern: schemaCurrencyPattern},
			schemaCurrencyScale: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: agentCurrencyScaleMax,
			},
			eventSchemaClosesAt: map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
			eventSchemaPaymentWindow: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
				schemaMaximum: math.MaxInt32,
			},
			"pricing":              nullableEventPolicy(eventTierPolicySchema()),
			"details_requirements": nullableEventPolicy(eventDetailsRequirementsSchema()),
		},
	}
}

func eventChangeDocumentSchema() map[string]any {
	return map[string]any{
		manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{ordersSchemaName, schemaTimezone, schemaStartsAt, schemaEndsAt, eventSchemaModules},
		schemaProperties: map[string]any{
			ordersSchemaName: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: events.MaxNameLength,
			},
			schemaTimezone: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: events.MaxModuleIDLength,
			},
			schemaStartsAt: map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
			schemaEndsAt:   map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
			eventSchemaModules: map[string]any{
				manifestType:           schemaArray,
				ordersSchemaMaxItems:   events.MaxModules,
				eventSchemaUniqueItems: true,
				schemaItems: map[string]any{
					manifestType:    schemaString,
					schemaMaxLength: events.MaxModuleIDLength,
					schemaPattern:   "^[a-z][a-z0-9_.-]*$",
				},
			},
			"registration": nullableEventPolicy(eventRegistrationRulesSchema()),
			schemaDeposit:  nullableEventPolicy(depositPolicySchema(false)),
		},
	}
}

func eventChangeMethod() map[string]any {
	method := administrativeRead(
		"change_event",
		"Create or fully replace this deployment's event configuration only after"+
			" an explicit global-administrator request and exact approval. Read event"+
			" first for replacement; supply explicit id, expected_version (0 creates)"+
			" and the FULL document. Times must be absolute instants plus an IANA dis"+
			"play timezone. Module IDs are syntactically validated, not a built-in al"+
			"lowlist. Omitting/null registration or deposit REMOVES that policy; omit"+
			"ted modules are disabled. No registration, pass assignment/cancellation,"+
			" frozen pass terms, allocation or money is changed. Five-minute proposal"+
			", expiry/version/current actor rechecked at execution. Reconcile the SAM"+
			"E operation after uncertainty; not_found never proves safe replacement. "+
			"Original result is historical even after later edits. Operator operation"+
			" IDs are a separate namespace.",
		map[string]any{
			"id":                  map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
			schemaDocument:        eventChangeDocumentSchema(),
		},
		[]string{"id", schemaExpectedVersion, schemaDocument},
		[]string{"id", schemaVersion, schemaDocument},
	)
	method[manifestEffect], method[manifestIdempotency] = effectWrite, true
	method[manifestMaxResult] = eventChangeResultLimit
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = eventChangePath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode: assignmentConfirmationMode, manifestPreparePath: eventChangePath + "/prepare",
	}
	return method
}

func eventDetailsRequirementsSchema() map[string]any {
	return map[string]any{
		manifestType: schemaObject, schemaAdditionalProperties: false, schemaRequired: []string{},
		schemaProperties: map[string]any{
			"legal_name":      map[string]any{manifestType: schemaBoolean},
			"passport_number": map[string]any{manifestType: schemaBoolean},
		},
	}
}
