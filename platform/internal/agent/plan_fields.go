package agent

import (
	"bytes"
	"errors"
	"slices"
)

const planActionField = "action"
const planOrderIDField = "order_id"
const planMediaField = "media_action"
const planOrderField = "order_action"
const planProfileField = "profile_action"
const planViewField = "view"
const planKnowledgeField = "knowledge_action"
const planRegistrationField = "registration_action"
const mediaRegistrationField = "registration_event"
const registrationAssignmentField = "assignment"

// Optional action fields remain compatible with the internal model endpoint.
// Every supplied key must still have exact spelling and occur only once.
func validatePlanFields(data []byte, kind string) error {
	if kind == planLineupField {
		return codexLineupFields(data)
	}
	allowed := map[string][]string{
		"": {
			planLineupField,
			codexTextField,
			planViewField,
			planActionField,
			planOrderField,
			planProfileField,
			planMediaField,
			planKnowledgeField,
			planScriptField,
			planHistoryField,
			planRegistrationField,
		},
		planRegistrationField: {
			registrationAssignmentField,
			codexNameField,
			proposalEventField,
			"target",
			"invite_telegram_id",
			proposalPaymentAdminField,
			"view",
			proposalCursorField,
		},
		registrationAssignmentField: {
			"total_price",
			"kind",
			"comment",
			"skip_balance",
			"append_tier",
			assignmentCreateField,
			assignmentProfileField,
			providerRoleField,
			profileLegalNameField,
		},
		planHistoryField: {"before"},
		planLineupField:  {"scope", "date", "room", "dj", "cursor"},
		planScriptField:  {"code", "input_json"},
		planKnowledgeField: {
			codexNameField,
			proposalEventField,
			"topic",
			"fact_key",
			codexTextField,
			"proposal_id",
			"review_queue",
			proposalCursorField,
		},
		planActionField:  {codexNameField, "slot_id"},
		planOrderField:   {codexNameField, planOrderIDField, "extra"},
		planProfileField: {codexNameField, "field", "value"},
		planMediaField: {"media_id", "intent", "amount", "currency",
			planOrderIDField, mediaRegistrationField, "food_kind", "start_ms", "end_ms", "frame_count"},
	}
	fields, err := codexObject(data)
	if err != nil {
		return errors.New("invalid plan object")
	}
	for key, value := range fields {
		if !slices.Contains(allowed[kind], key) {
			return errors.New("unknown plan field")
		}
		if kind == "" && key != codexTextField && key != planViewField && !bytes.Equal(value, []byte("null")) {
			if err = validatePlanFields(value, key); err != nil {
				return err
			}
		}
		if kind == planRegistrationField && key == registrationAssignmentField && !bytes.Equal(value, []byte("null")) {
			if err = validatePlanFields(value, key); err != nil {
				return err
			}
		}
	}
	return nil
}
