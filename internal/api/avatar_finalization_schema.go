package api

import "github.com/complynx/zns-chatbot/internal/db"

const schemaAvatarDocument = "avatar"
const schemaAvatarDeliveryState = "delivery_state"
const schemaAvatarProcessorVersion = "processor_version"

func avatarFinalizationReceiptSchema() map[string]any {
	result := avatarReceiptSchema()
	properties, _ := result[schemaProperties].(map[string]any)
	delete(properties, schemaTaskID)
	delete(properties, schemaSourceFileID)
	properties[schemaDraftID] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	properties["draft_version"] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	result[schemaRequired] = []string{
		schemaArtifactID,
		schemaAvatarDeliveryID,
		schemaDraftID,
		"draft_version",
		schemaSourceSHA256,
		schemaFrameSHA256,
		schemaAvatarProcessorVersion,
		schemaAvatarTransform,
		schemaSHA256,
		schemaImageWidth,
		schemaImageHeight,
		schemaSizeBytes,
		schemaCreatedAt,
	}
	return result
}
func avatarFinalizationViewSchema() map[string]any {
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{schemaAvatarDocument, schemaAvatarDeliveryState, "delivery_version"},
		schemaProperties: map[string]any{
			schemaAvatarDocument: avatarFinalizationReceiptSchema(),
			schemaAvatarDeliveryState: map[string]any{
				manifestType: schemaString,
				schemaEnum: []string{
					agentPending,
					notificationStateRetryable,
					notificationStateInflight,
					notificationStateDelivered,
					notificationStateUnknown,
					notificationStateFailed,
					notificationStateBlocked,
					notificationStateDismissed,
				},
			},
			"delivery_version": map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		},
	}
}

func avatarFinalizationMethods(
	method func(string, string, map[string]any, map[string]any) map[string]any,
	input func(string) map[string]any,
) []any {
	args := map[string]any{
		manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{schemaDraftID, schemaExpectedVersion},
		schemaProperties: map[string]any{
			schemaDraftID:         map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		},
	}

	finalize := method(
		"finalize_draft",
		"Complete an explicit current avatar request by finalizing the saved draft_id at "+
			"expected_version into an immutable JPEG and queueing its Telegram document. "+
			"Existing editing-only or preview-only requests do not authorize finalization. "+
			"Needs no active Loom run. Success proves persistence and delivery admission, not "+
			"delivery. Read finalized with the returned artifact_id for current delivery; "+
			"say sent only when delivery_state is delivered, otherwise describe queued or failed "+
			"status. A saved draft, preview or imported model artifact is not a delivered document. "+
			"Reconcile unknown HTTP replies with finalization_operation using the same operation ID "+
			"and exact payload, even after later edits. "+
			"Never finalize merely because a photo/draft exists, never regenerate to retry unknown "+
			"delivery, and never set the Telegram profile photo.",
		args,
		avatarFinalizationReceiptSchema(),
	)
	finalize[manifestEffect] = effectWrite
	finalize[manifestIdempotency] = true
	finalize[manifestTimeout] = avatarUploadTimeout.Milliseconds()
	finalize[manifestStatusPath] = avatarPath + "finalization_operation"
	finalize[manifestDedupWindow] = agentRegistrationDedupSeconds
	operation := method(
		"finalization_operation",
		"Read the immutable saved-draft finalization receipt by arguments.operation_id, or the "+
			"operation_id-only status envelope. Not found means no known committed receipt, not "+
			"permission to use a new operation. This receipt is not current delivery status.",
		input("operation_id"),
		avatarFinalizationReceiptSchema(),
	)
	get := method(
		"finalized",
		"Read your finalized saved-draft artifact and current delivery state/version. "+
			"Historical receipts are immutable. Unknown delivery is not proof of non-delivery; "+
			"never resend or generate a replacement blindly. No administrator bypass.",
		input(schemaArtifactID),
		avatarFinalizationViewSchema(),
	)
	fetch := method(
		"fetch_finalized",
		"Explicitly import your immutable saved-draft JPEG into Loom file tools by artifact_id. "+
			"Bytes stay in Zinusya behind current owner authorization; "+
			"no base64 or image bytes in conversation JSON.",
		input(schemaArtifactID),
		avatarFinalizationReceiptSchema(),
	)
	fetch[manifestDisclosure] = true
	fetch[manifestMIMETypes] = []string{mimeJPEG}
	list := method(
		"finalized_list",
		"List your retained saved-draft avatar documents, newest first, with current delivery "+
			"state/version. Metadata only, at most twenty artifacts across the shared quota. "+
			"Recover locators here after losing local browser state; "+
			"do not regenerate to recover a download. No legacy task-based receipts or operational "+
			"attempt data are returned.",
		input(""),
		map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{"avatars"},
			schemaProperties: map[string]any{
				"avatars": map[string]any{
					manifestType:         schemaArray,
					schemaItems:          avatarFinalizationViewSchema(),
					ordersSchemaMaxItems: db.MaxAvatarsPerOwner,
				},
			},
		},
	)
	list[manifestMaxResult] = agentAdminResultLimit
	return []any{finalize, operation, get, fetch, list}
}
