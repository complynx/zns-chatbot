package api

const schemaDraftID = "draft_id"
const schemaDigestPattern = "^[a-f0-9]{64}$"
const schemaImageWidth = "width"
const schemaImageHeight = "height"

func avatarPreviewSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	positive := map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	digest := map[string]any{manifestType: schemaString, schemaPattern: schemaDigestPattern}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			schemaDraftID,
			schemaVersion,
			schemaSourceSHA256,
			schemaFrameSHA256,
			schemaAvatarTransform,
			schemaSHA256,
			schemaSizeBytes,
			schemaImageWidth,
			schemaImageHeight,
		},
		schemaProperties: map[string]any{
			schemaDraftID:         uuidField,
			schemaVersion:         positive,
			schemaSourceSHA256:    digest,
			schemaFrameSHA256:     digest,
			schemaAvatarTransform: avatarTransformSchema(),
			schemaSHA256:          digest,
			schemaSizeBytes:       positive,
			schemaImageWidth:      positive,
			schemaImageHeight:     positive,
		},
	}
}

func avatarPreviewMethod(
	method func(string, string, map[string]any, map[string]any) map[string]any,
	previewInput, uuidField map[string]any,
) map[string]any {
	previewInput[schemaRequired] = []string{schemaDraftID, schemaExpectedVersion}
	previewInput[schemaProperties] = map[string]any{
		schemaDraftID:         uuidField,
		schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
	}
	preview := method(
		"preview_draft",
		"Render an authoritative read-only preview of the saved draft_id and expected_version. Returns a JPEG descriptor whose versioned download path rechecks current ownership, version and pinned frame. A changed draft rejects; reread it before requesting another preview. Creates no artifact, operation receipt, task or Telegram delivery. Old preview links conflict after a later edit. Use Zinusya file IDs/descriptors; never embed image bytes in conversation JSON.",
		previewInput,
		avatarPreviewSchema(),
	)
	preview[manifestDisclosure] = true
	preview[manifestMIMETypes] = []string{mimeJPEG}
	preview[manifestTimeout] = avatarUploadTimeout.Milliseconds()
	return preview
}
