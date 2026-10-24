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
			"source_expires_at":       instant,
			"source_expired":          map[string]any{manifestType: "boolean"},
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
		"When saved editing is needed, select exactly one owned source_file_id or"+
			" history_source with record_id and archive_id; pin its digest and size a"+
			"nd create an identity edit draft. Intermediate editing only: does not ge"+
			"nerate or send an avatar. For a plain explicit avatar request prefer gen"+
			"erate. If you create a draft to fulfill that request, continue to finali"+
			"ze_draft with the returned id and version; do not finish with a created/"+
			"sent claim. Stop at a draft only when the user requested editing or prev"+
			"iew alone. No active Loom run required. Reconcile lost responses with dr"+
			"aft_operation using the same operation ID; committed operation receipts "+
			"survive source loss, but preview and new finalization need Telegram acce"+
			"ss. Try fetch_original for the saved draft before asking for the photo a"+
			"gain; an expired model artifact handle does not prove Telegram source lo"+
			"ss. Telegram originals are not stored. Admission limit: five active draf"+
			"ts and 50 MiB of browser sources per owner, plus the application-wide br"+
			"owser byte quota. Browser sources expire from creation time under DB set"+
			"tings; current metadata reports source_expires_at/source_expired. Expire"+
			"d sources reject new edits, reads, preview and finalization; immutable r"+
			"eceipts and completed JPEGs remain available.",
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
		"Save a complete canonical transform using the currently read expected_ve"+
			"rsion. Shares state with the native form, needs no active Loom run and d"+
			"oes not generate/send. Stale versions reject; reread draft. Retry exact "+
			"uncertain operations using the same operation ID; draft_operation return"+
			"s their original receipt even after later edits. PPM/millidegree units a"+
			"nd positive directions match generate.",
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
		"Read your current saved avatar editing state by draft_id before continui"+
			"ng from a native form or after a version conflict. Draft IDs are owner-s"+
			"coped locators, not authority.",
		input("draft_id"),
		avatarDraftSchema(),
	)
	list := method(
		"drafts",
		"List your saved avatar edit metadata, newest edited first. Recover earli"+
			"er Telegram photos here using source_file_id or fetch_original by draft "+
			"id; try the source before asking for a resend. Telegram inputs are refer"+
			"ences and may require the person to resend a lost source. Browser-upload"+
			" originals are retained. Metadata and committed operation receipts remai"+
			"n readable independently of source availability.",
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
		"Read the immutable original successful draft create/save receipt by argu"+
			"ments.operation_id. A missing operation is unknown; preserve the same op"+
			"eration ID and payload for recovery. It does not report current draft ve"+
			"rsion or delivery.",
		input("operation_id"),
		avatarDraftSchema(),
	)
	fetch := method(
		"fetch_original",
		"Explicitly import the selected original into Loom file tools by draft_id"+
			". Telegram inputs are downloaded on demand and may be unavailable; brows"+
			"er uploads are retained. The owner-authorized descriptor references an a"+
			"uthenticated Zinusya endpoint; never put image bytes in conversation JSO"+
			"N.",
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
