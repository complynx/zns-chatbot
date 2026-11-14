package api

import "github.com/complynx/zns-chatbot/internal/db"

const profileSchemaGender = "gender"

const profileGenderMaxLength = 128

func profileGenderMethod(version map[string]any) map[string]any {
	return profileMethod("set_gender",
		"Save your own gender and/or pronouns/address preferences as short free text, including nonbinary "+
			"or language-specific forms. Use source=explicit for what the user stated; when unknown and required "+
			"by the language, infer from their name or available data and use source=inferred. Do not treat an "+
			"inference as confirmed identity. Explicit preferences take priority; inferred writes cannot overwrite "+
			"explicit ones for the supplied field. Read the current profile/version first. "+
			"Gender/pronouns appear in basic directory cards "+
			"under the user's visibility rules. Explicit and inferred preferences are public when "+
			"visible=true, with separate gender_source and pronouns_source labels. Supply at least one field, "+
			"and only fields that this statement confirms or infers: confirming pronouns does not confirm gender. "+
			"Omitted fields keep their own value and source. Guessed pronouns are cleared if the gender guess changes "+
			"or becomes explicit, unless new pronouns are supplied. An empty string clears that field and its source. "+
			"A forwarded author's gender is not the forwarding user's gender. "+
			"For another person, use their info card; this tool always edits the authenticated user's own profile.",
		map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
			schemaProperties: map[string]any{
				schemaExpectedVersion: version,
				profileSchemaGender:   map[string]any{manifestType: schemaString, schemaMaxLength: profileGenderMaxLength},
				"pronouns":            map[string]any{manifestType: schemaString, schemaMaxLength: profileGenderMaxLength},
				massageSchemaSource: map[string]any{
					manifestType: schemaString,
					schemaEnum:   []string{db.GenderSourceExplicit, db.GenderSourceInferred},
				},
			}, schemaRequired: []string{schemaExpectedVersion, massageSchemaSource}}, true)
}
