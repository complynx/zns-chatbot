package api

import "github.com/complynx/zns-chatbot/internal/db"

const schemaAvatarHistorySource = "history_source"

const schemaSourceSHA256 = "source_sha256"
const schemaFrameSHA256 = "frame_sha256"
const schemaAvatarTransform = "transform"
const schemaMIMEType = "mime_type"
const mimePNG = "image/png"

func avatarDraftSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	digest := map[string]any{manifestType: schemaString, schemaPattern: "^[a-f0-9]{64}$"}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			"id",
			schemaSourceSHA256,
			schemaMIMEType,
			"size_bytes",
			schemaFrameSHA256,
			schemaAvatarTransform,
			schemaVersion,
			schemaCreatedAt,
			"updated_at",
		},
		schemaProperties: map[string]any{
			"id":                      uuidField,
			"source_file_id":          uuidField,
			schemaAvatarHistorySource: avatarHistorySourceSchema(),
			schemaSourceSHA256:        digest,
			schemaFrameSHA256:         digest,
			schemaMIMEType:            map[string]any{manifestType: schemaString, "enum": []string{mimeJPEG, mimePNG}},
			"size_bytes": map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
			},
			schemaAvatarTransform: avatarTransformSchema(),
			schemaVersion: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
			},
			schemaCreatedAt: instant,
			"updated_at":    instant,
		},
	}
}
func avatarDraftMethods() []any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	input := func(name string) map[string]any {
		required := []string{}
		fields := map[string]any{}
		if name != "" {
			required = append(required, name)
			fields[name] = uuidField
		}
		return map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             required,
			schemaProperties:           fields,
		}
	}
	method := func(id, description string, args, output map[string]any) map[string]any {
		return map[string]any{
			"id":                 id,
			manifestDescription:  description,
			manifestPermissions:  []string{},
			manifestPath:         avatarPath + id,
			manifestEffect:       effectRead,
			manifestTimeout:      toolTimeoutMillis,
			manifestMaxResult:    toolResultLimit,
			manifestInputSchema:  args,
			manifestOutputSchema: output,
			manifestIdempotency:  false,
			manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
			manifestMIMETypes:    []string{},
			manifestDisclosure:   false,
		}
	}
	create := method(
		"create_draft",
		"After an explicit avatar editing request, select exactly one owned source_file_id or history_source with record_id and archive_id; pin its digest and size and create an identity edit draft. Does not generate or send an avatar. No active Loom run required. Reconcile lost responses with draft_operation using the same operation ID; committed operation receipts survive source loss, but preview and new finalization need Telegram access. If unavailable ask for the photo again. Telegram originals are not stored. Admission limit: five drafts; browser uploads retain at most 50 MiB per owner.",
		avatarDraftCreateSchema(),
		avatarDraftSchema(),
	)
	saveInput := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{"draft_id", schemaExpectedVersion, schemaAvatarTransform},
		schemaProperties: map[string]any{
			"draft_id":            uuidField,
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			schemaAvatarTransform: avatarTransformSchema(),
		},
	}

	save := method(
		"save_draft",
		"Save a complete canonical transform using the currently read expected_version. Shares state with the native form, needs no active Loom run and does not generate/send. Stale versions reject; reread draft. Retry exact uncertain operations using the same operation ID; draft_operation returns their original receipt even after later edits. PPM/millidegree units and positive directions match generate.",
		saveInput,
		avatarDraftSchema(),
	)
	for _, m := range []map[string]any{create, save} {
		m[manifestEffect] = effectWrite
		m[manifestIdempotency] = true
		m[manifestStatusPath] = avatarPath + "draft_operation"
		m[manifestDedupWindow] = agentRegistrationDedupSeconds
	}
	create[manifestTimeout] = avatarUploadTimeout.Milliseconds()
	get := method(
		"draft",
		"Read your current saved avatar editing state by draft_id before continuing from a native form or after a version conflict. Draft IDs are owner-scoped locators, not authority.",
		input("draft_id"),
		avatarDraftSchema(),
	)
	list := method(
		"drafts",
		"List your saved avatar edit metadata, newest edited first. Telegram inputs are references and may require the person to resend a lost source. Browser-upload originals are retained. Metadata and committed operation receipts remain readable independently of source availability.",
		input(""),
		map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{"drafts"},
			schemaProperties: map[string]any{
				"drafts": map[string]any{
					manifestType:         schemaArray,
					schemaItems:          avatarDraftSchema(),
					ordersSchemaMaxItems: db.MaxAvatarDraftsPerOwner,
				},
			},
		},
	)
	op := method(
		"draft_operation",
		"Read the immutable original successful draft create/save receipt by arguments.operation_id. A missing operation is unknown; preserve the same operation ID and payload for recovery. It does not report current draft version or delivery.",
		input("operation_id"),
		avatarDraftSchema(),
	)
	fetch := method(
		"fetch_original",
		"Explicitly import the selected original into Loom file tools by draft_id. Telegram inputs are downloaded on demand and may be unavailable; browser uploads are retained. The owner-authorized descriptor references an authenticated Zinusya endpoint; never put image bytes in conversation JSON.",
		input("draft_id"),
		avatarDraftSchema(),
	)
	fetch[manifestDisclosure] = true
	fetch[manifestMIMETypes] = []string{mimeJPEG, mimePNG}
	return []any{create, save, get, list, op, fetch, avatarPreviewMethod(method, input("draft_id"), uuidField)}
}

func avatarHistorySourceSchema() map[string]any {
	return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{schemaHistoryRecordID, schemaArchiveID}, schemaProperties: map[string]any{
			schemaHistoryRecordID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaArchiveID:       map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		}}
}
func avatarDraftCreateSchema() map[string]any {
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaProperties: map[string]any{
			schemaSourceFileID:        map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaAvatarHistorySource: avatarHistorySourceSchema(),
		},
		"oneOf": []map[string]any{
			{schemaRequired: []string{schemaSourceFileID}},
			{schemaRequired: []string{schemaAvatarHistorySource}},
		},
	}
}
