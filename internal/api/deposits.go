package api

import "github.com/complynx/zns-chatbot/internal/registrations"

const (
	schemaDeposit       = "deposit"
	schemaMinimumMinor  = "minimum_minor"
	schemaMinimumBPS    = "minimum_bps"
	schemaRequiredMinor = "required_minor"
	schemaFinalDueAt    = "final_due_at"
)

func depositPolicySchema(snapshot bool) map[string]any {
	amount := map[string]any{manifestType: schemaInteger, schemaMinimum: 0, schemaMaximum: agentJSONIntegerMax}
	properties := map[string]any{
		schemaMinimumMinor: amount,
		schemaMinimumBPS: map[string]any{
			manifestType:  schemaInteger,
			schemaMinimum: 0,
			schemaMaximum: registrations.BasisPoints,
		},
	}
	required := []string{schemaMinimumMinor, schemaMinimumBPS}
	if snapshot {
		properties[schemaRequiredMinor] = amount
		properties[schemaFinalDueAt] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
		required = append(required, schemaRequiredMinor, schemaFinalDueAt)
	}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             required,
		schemaProperties:           properties,
	}
}
