package api

import "github.com/complynx/zns-chatbot/internal/avatar"

func avatarTransformSchema() map[string]any {
	number := func(low, high int64) map[string]any {
		return map[string]any{manifestType: schemaInteger, schemaMinimum: low, "maximum": high}
	}
	return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{"offset_x_ppm", "offset_y_ppm", "zoom_ppm", "angle_millidegrees"},
		schemaProperties: map[string]any{
			"offset_x_ppm":       number(-avatar.MaxOffsetPPM, avatar.MaxOffsetPPM),
			"offset_y_ppm":       number(-avatar.MaxOffsetPPM, avatar.MaxOffsetPPM),
			"zoom_ppm":           number(avatar.MinZoomPPM, avatar.MaxZoomPPM),
			"angle_millidegrees": number(-avatar.HalfTurnMilli, avatar.HalfTurnMilli),
		}}
}

func avatarGenerateSchema() map[string]any {
	return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{schemaSourceFileID}, schemaProperties: map[string]any{
			schemaSourceFileID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			"transform":        avatarTransformSchema()}}
}
