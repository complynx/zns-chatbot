package agent

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const (
	visibilityQueue        = "queue"
	visibilityPaymentQueue = "payment_queue"
	visibilityAdmins       = "admins"
	visibilityHome         = "home"
	visibilityEvents       = "events"
	visibilityInvitations  = "invitations"
	visibilityPayment      = "payment"
	schemaEnum             = "enum"
	schemaType             = "type"
	schemaProperties       = "properties"
)

const schemaString = "string"
const schemaNull = "null"

// CanRegistrationAction uses only current host capability evidence. Read and show
// expose ordinary discovery; their privileged views have a separate check.
func CanRegistrationAction(input Input, event, name string) bool {
	if name == RegistrationRead || name == RegistrationShow {
		return true
	}
	if input.Registration != nil {
		for _, capability := range input.Registration.Capabilities {
			if capability.Event == event && slices.Contains(capability.Actions, name) {
				return true
			}
		}
	}
	return false
}

func CanRegistrationView(input Input, event, view string) bool {
	switch view {
	case visibilityQueue:
		return CanRegistrationAction(input, event, RegistrationAdminAssign)
	case visibilityPaymentQueue:
		return CanRegistrationAction(input, event, "proof_accept")
	case RegistrationAdminTarget:
		return CanRegistrationAction(input, event, RegistrationAdminAssign)
	case RegistrationTakeoverTarget:
		return CanRegistrationAction(input, event, "takeover")
	case "", visibilityHome, visibilityEvents, visibilityInvitations, visibilityAdmins, ProfilesView, visibilityPayment:
		return true
	default:
		return false
	}
}

func CanKnowledgeAction(input Input, event, name string) bool {
	switch name {
	case knowledge.Curate, knowledge.RemoveFact, knowledgeReviewCard:
		if input.Knowledge != nil {
			for _, scope := range input.Knowledge.Scopes {
				if scope.Event == event {
					if name == knowledgeReviewCard {
						return scope.CanReview
					}
					return scope.CanCurate
				}
			}
		}
		return false
	case "read", "proposals", "memo_read", "suggest", "memo_set", "memo_delete":
		return true
	default:
		return false
	}
}

func visibleCapability(input Input, name string) bool {
	switch name {
	case "book":
		return canBook(input)
	case "order_export":
		return canExportOrders(input)
	}
	if input.Registration != nil {
		for _, capability := range input.Registration.Capabilities {
			if CanRegistrationAction(input, capability.Event, name) {
				return true
			}
		}
	}
	if input.Knowledge != nil {
		for _, scope := range input.Knowledge.Scopes {
			switch name {
			case knowledge.Curate, knowledge.RemoveFact, knowledgeReviewCard:
				if CanKnowledgeAction(input, scope.Event, name) {
					return true
				}
			}
		}
	}
	return false
}

func visibleSkillBody(input Input, body string) string {
	var result strings.Builder
	include := true
	for _, line := range strings.SplitAfter(body, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "<!-- capability:"); ok {
			include = visibleCapability(input, strings.TrimSuffix(name, " -->"))
		} else if strings.TrimSpace(line) == "<!-- end -->" {
			include = true
		} else if include {
			result.WriteString(line)
		}
	}
	return result.String()
}

// Project the canonical schema after the freshness hook, immediately before each
// request. The full schema remains private decoding vocabulary, never discovery.
func visiblePlanSchema(input Input) (string, error) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(planSchema), &schema); err != nil {
		return "", err
	}
	properties, ok := schema[schemaProperties].(map[string]any)
	if !ok {
		return "", errors.New("invalid plan schema properties")
	}
	if !canBook(input) {
		properties["action"] = map[string]any{schemaType: schemaNull}
	}
	orderFields := actionProperties(properties, "order_action")
	if orderFields == nil {
		return "", errors.New("invalid order action schema")
	}
	orderFields["name"] = map[string]any{schemaType: schemaString, schemaEnum: visibleOrderNames(input)}
	if !canBook(input) {
		properties["order_action"] = map[string]any{schemaType: schemaNull}
	}
	registration := actionProperties(properties, "registration_action")
	knowledgeFields := actionProperties(properties, "knowledge_action")
	if registration == nil || knowledgeFields == nil {
		return "", errors.New("invalid plan action schema")
	}
	registration["name"] = map[string]any{schemaType: schemaString, schemaEnum: registrationNames(input)}
	views := []string{
		"",
		visibilityHome,
		visibilityEvents,
		visibilityInvitations,
		visibilityAdmins,
		ProfilesView,
		visibilityPayment,
	}
	for view, action := range map[string]string{visibilityQueue: RegistrationAdminAssign, visibilityPaymentQueue: "proof_accept", RegistrationAdminTarget: RegistrationAdminAssign, RegistrationTakeoverTarget: "takeover"} {
		if visibleCapability(input, action) {
			views = append(views, view)
		}
	}
	slices.Sort(views)
	registration["view"] = map[string]any{schemaType: schemaString, schemaEnum: views}
	if !visibleCapability(input, RegistrationAdminAssign) {
		registration["assignment"] = map[string]any{schemaType: schemaNull}
	}
	names := []string{"read", "proposals", "memo_read", "suggest", "memo_set", "memo_delete"}
	for _, name := range []string{knowledge.Curate, knowledge.RemoveFact, knowledgeReviewCard} {
		if visibleCapability(input, name) {
			names = append(names, name)
		}
	}
	knowledgeFields["name"] = map[string]any{schemaType: schemaString, schemaEnum: names}
	if !visibleCapability(input, knowledgeReviewCard) {
		knowledgeFields["review_queue"] = map[string]any{schemaType: "boolean", schemaEnum: []bool{false}}
	}
	body, err := json.Marshal(schema)
	return string(body), err
}

func actionProperties(properties map[string]any, name string) map[string]any {
	action, ok := properties[name].(map[string]any)
	if !ok {
		return nil
	}
	variants, ok := action["anyOf"].([]any)
	if !ok || len(variants) != 2 {
		return nil
	}
	object, ok := variants[1].(map[string]any)
	if !ok {
		return nil
	}
	fields, _ := object[schemaProperties].(map[string]any)
	return fields
}

func registrationNames(input Input) []string {
	names := []string{RegistrationRead, RegistrationShow}
	if input.Registration != nil {
		for _, capability := range input.Registration.Capabilities {
			names = append(names, capability.Actions...)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// A model can guess hidden names. Reject those proposals before execution as well
// as retaining the independent business API authorization checks.
func validateVisiblePlan(input Input, plan Plan) error {
	if plan.Action != nil && !canBook(input) {
		return errors.New("booking capability unavailable")
	}
	if plan.OrderAction != nil && !slices.Contains(visibleOrderNames(input), plan.OrderAction.Name) {
		return errors.New("order capability unavailable")
	}
	if action := plan.RegistrationAction; action != nil {
		if !CanRegistrationAction(input, action.Event, action.Name) ||
			!CanRegistrationView(input, action.Event, action.View) {
			return errors.New("registration capability unavailable")
		}
	}
	if action := plan.KnowledgeAction; action != nil {
		if !CanKnowledgeAction(input, action.Event, action.Name) ||
			(action.ReviewQueue && !CanKnowledgeAction(input, action.Event, knowledgeReviewCard)) {
			return errors.New("knowledge capability unavailable")
		}
	}
	return nil
}

func visibleProviderInput(input Input) Input {
	input = projectModernOrders(input)
	if input.Registration != nil {
		value := *input.Registration
		value.Reads = nil
		for _, read := range input.Registration.Reads {
			if CanRegistrationAction(input, read.Request.Event, read.Request.Name) &&
				CanRegistrationView(input, read.Request.Event, read.Request.View) {
				value.Reads = append(value.Reads, read)
			}
		}
		input.Registration = &value
	}
	if input.Knowledge != nil {
		value := *input.Knowledge
		value.Reads = nil
		for _, read := range input.Knowledge.Reads {
			if CanKnowledgeAction(input, read.Request.Event, read.Request.Name) &&
				(!read.Request.ReviewQueue || CanKnowledgeAction(input, read.Request.Event, knowledgeReviewCard)) {
				value.Reads = append(value.Reads, read)
			}
		}
		input.Knowledge = &value
	}
	return input
}
