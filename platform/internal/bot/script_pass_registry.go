package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const registrationSchemaType = "type"
const registrationSchemaString = "string"
const registrationSchemaInteger = "integer"
const registrationSchemaBoolean = "boolean"
const registrationOperationID = "operation_id"
const registrationAdminsView = "admins"
const passBatchRecalculate = "recalculate"
const scriptPassShow = "passes.registration.show"
const scriptPassRead = "passes.registration.read"
const scriptPassAdminRead = "passes.admin.queue"
const scriptPassAdminTarget = "passes.admin.target"
const scriptPassReviewRead = "passes.payments.review"
const scriptPassTakeoverRead = "passes.takeover.read"
const scriptPassTiers = "passes.tiers"
const scriptPassExport = "passes.export"
const scriptPassResume = "passes.resume"
const scriptPassOperations = "passes.operations"
const scriptPassBatchAssign = "passes.batch.assign"
const scriptPassBatchCancel = "passes.batch.cancel"
const scriptRegistrationBatchUncouple = "passes.batch.uncouple"
const scriptPassResultLimit = 32 << 10
const registrationCapabilitiesPath = "/v1/passes/tool-capabilities"

func passToolActions() map[string]string {
	return map[string]string{
		"passes.registration.solo":          "solo",
		"passes.registration.invite":        passInvite,
		"passes.registration.accept":        passAccept,
		"passes.registration.decline":       passDecline,
		"passes.registration.cancel":        "cancel",
		"passes.registration.payment_admin": "payment_admin",
		"passes.admin.assign":               passBatchAssign,
		"passes.admin.cancel":               passBatchCancel,
		"passes.admin.uncouple":             passBatchUncouple,
		"passes.admin.recalculate":          passBatchRecalculate,
		"passes.payments.accept":            "proof_accept",
		"passes.payments.reject":            "proof_reject",
		"passes.takeover.apply":             "takeover",
		"passes.takeover.received_only":     "received_only",
		scriptPassAdminRead:                 passBatchAssign,
		scriptPassAdminTarget:               passBatchAssign,
		scriptPassReviewRead:                "proof_accept",
		scriptPassTakeoverRead:              "takeover",
		scriptPassTiers:                     passBatchCancel,
		scriptPassBatchAssign:               passBatchAssign,
		scriptPassBatchCancel:               passBatchCancel,
		scriptRegistrationBatchUncouple:     passBatchUncouple,
	}
}

func (b *Bot) scriptPassEntries(ctx context.Context, owner string) ([]scriptToolEntry, error) {
	var capabilities passbooking.ToolCapabilities
	if err := b.API.call(ctx, owner, http.MethodGet, registrationCapabilitiesPath, nil, &capabilities); err != nil {
		return nil, err
	}
	names := []string{scriptPassRead, scriptPassShow, scriptPassOperations, scriptPassResume}
	for name, action := range passToolActions() {
		if name == "passes.admin.cancel" {
			action = passBatchAssign
		}
		if slices.Contains(capabilities.Actions, action) {
			names = append(names, name)
		}
	}
	if capabilities.Export {
		names = append(names, scriptPassExport)
	}
	slices.Sort(names)
	entries := make([]scriptToolEntry, 0, len(names))
	for _, name := range names {
		entries = append(
			entries,
			scriptToolEntry{
				descriptor:  passToolDescriptor(name),
				prepare:     b.preparePassTool,
				execute:     b.executePassTool,
				resultLimit: scriptPassResultLimit,
			},
		)
	}
	return entries, nil
}

func passToolDescriptor(name string) scriptclient.Tool {
	properties := map[string]any{knowledgeEventQuery: map[string]any{registrationSchemaType: registrationSchemaString}}
	required := []string{knowledgeEventQuery}
	description := "Execute the named pass operation using completed registration reads. Host owns actor, versions and replay identity. Upload proof through Telegram; never supply proof IDs."
	switch name {
	case scriptPassRead, scriptPassShow:
		properties["view"] = map[string]any{"enum": []string{"home", "invitations", registrationAdminsView, "payment"}}
		properties["cursor"] = map[string]string{registrationSchemaType: registrationSchemaString}
		description = "Read your pass booking and selected details. At most three registration reads per update; use returned next cursor only. These reads ground subsequent registration operations."
	case scriptPassAdminRead, scriptPassReviewRead:
		properties["cursor"] = map[string]string{registrationSchemaType: registrationSchemaString}
		description = "Read one authorized queue page and ground observed target versions for subsequent actions. Use only returned next cursor."
	case scriptPassAdminTarget, scriptPassTakeoverRead:
		properties["target"] = map[string]string{
			registrationSchemaType: registrationSchemaString,
			"description":          "Telegram ID explicitly requested by the user or shown in an authorized queue.",
		}
		required = append(required, "target")
	case scriptPassTiers:
		properties["cursor"] = map[string]string{registrationSchemaType: registrationSchemaString}
		description = "Read authorized pass tier and balance status as bounded JSON chunks. Follow next_cursor while more is true."
	case scriptPassExport, scriptPassOperations:
		properties = map[string]any{}
		required = []string{}
		description = "List recent owner-private pass operation references for exact replay after interruption. No command authority is granted by a reference."
		if name == scriptPassExport {
			description = "Deliver passes.xlsx for all currently authorized active pass events to this Telegram chat. No spreadsheet bytes enter model results. Repeated delivery in the same update uses its receipt."
		}
	case scriptPassResume:
		properties = map[string]any{
			registrationOperationID: map[string]string{registrationSchemaType: registrationSchemaString},
		}
		required = []string{registrationOperationID}
		description = "Resume exactly one host-saved pass operation. Its original event, versions, recipients and key remain unchanged. Current event-scoped authority is checked again."
	case scriptPassBatchAssign, scriptPassBatchCancel, scriptRegistrationBatchUncouple:
		properties["recipients"] = map[string]any{
			registrationSchemaType: "array",
			broadcastItemsKey:      map[string]string{registrationSchemaType: registrationSchemaInteger},
			"minItems":             1,
			"maxItems":             passbooking.MaxAdminBatchRecipients,
			"uniqueItems":          true,
		}
		required = append(required, "recipients")
		if name == scriptPassBatchAssign {
			properties["assignment"] = passAssignmentSchema()
		}
		description = "Run a durable batch for explicit Telegram recipients grounded in the current user request or an authorized queue. Uncouple accepts exactly one recipient. Per-recipient outcomes persist; resume the returned operation_id after interruption."
	default:
		properties["target"] = map[string]string{
			registrationSchemaType: registrationSchemaString,
			"description":          "Canonical owner from the appropriate host read.",
		}
		properties["invite_telegram_id"] = map[string]string{registrationSchemaType: registrationSchemaInteger}
		properties["payment_admin"] = map[string]string{registrationSchemaType: registrationSchemaString}
		if passToolActions()[name] == passBatchAssign {
			properties["assignment"] = passAssignmentSchema()
		}
	}
	if name == scriptPassShow {
		delete(properties, "cursor")
		description = "Show the native pass menu in this Telegram chat. Payment receipt upload remains host-owned and manual."
	}
	schema, _ := json.Marshal(
		map[string]any{
			registrationSchemaType: "object",
			"properties":           properties,
			"required":             required,
			"additionalProperties": false,
		},
	)
	return scriptclient.Tool{Name: name, Description: description, InputSchema: schema}
}

func passAssignmentSchema() map[string]any {
	return map[string]any{registrationSchemaType: "object", "additionalProperties": false, "properties": map[string]any{
		"total_price": map[string]string{
			registrationSchemaType: registrationSchemaInteger,
		},
		"kind":    map[string]string{registrationSchemaType: registrationSchemaString},
		"comment": map[string]string{registrationSchemaType: registrationSchemaString},
		"skip_balance": map[string]string{
			registrationSchemaType: registrationSchemaBoolean,
		},
		"append_tier": map[string]string{registrationSchemaType: registrationSchemaInteger},
		"create":      map[string]string{registrationSchemaType: registrationSchemaBoolean},
		"from_profile": map[string]string{
			registrationSchemaType: registrationSchemaBoolean,
		},
		"role":       map[string]any{"enum": []string{"leader", "follower"}},
		"legal_name": map[string]string{registrationSchemaType: registrationSchemaString},
	}}
}
