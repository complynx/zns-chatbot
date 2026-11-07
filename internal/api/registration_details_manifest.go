package api

const detailsOperationID = "operation_id"
const detailsLegalNameLimit = 256
const detailsPassportLimit = 128

const (
	registrationDetailsReadDescription = "Read only presence flags and version of your legal registration details. " +
		"These are separate from the profile name. Saved passport values are not returned or added to " +
		"general agent context."

	registrationPassportDescription = "Save only the passport/document number explicitly supplied for this registration " +
		"purpose. Do not repeat it in replies or unrelated context. " +
		"A chat submission already enters ordinary Telegram/Loom history; " +
		"dedicated storage and receipts protect their own copies. Read current version first. " +
		"Does not register or confirm payment."

	registrationDetailsOperationDescription = "Recover a historical redacted result using arguments.operation_id, " +
		"or the original envelope operation_id on the status endpoint. " +
		"Missing status is unknown: retain the same operation ID and exact arguments after uncertainty."
)

func registrationDetailsModule() map[string]any {
	version := map[string]any{manifestType: schemaInteger, schemaMinimum: 0}
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	input := func(properties map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaProperties:           properties,
			schemaRequired:             required,
		}
	}
	output := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{schemaVersion, "legal_name_present", "passport_number_present"},
		schemaProperties: map[string]any{
			schemaVersion:             version,
			"legal_name_present":      map[string]any{manifestType: schemaBoolean},
			"passport_number_present": map[string]any{manifestType: schemaBoolean},
		},
	}
	method := func(id, description string, arguments map[string]any, write bool) map[string]any {
		item := map[string]any{
			"id":                 id,
			manifestDescription:  description,
			manifestPermissions:  []string{},
			manifestPath:         registrationDetailsPath + id,
			manifestEffect:       effectRead,
			manifestTimeout:      toolTimeoutMillis,
			manifestMaxResult:    toolResultLimit,
			manifestInputSchema:  arguments,
			manifestOutputSchema: output,
			manifestIdempotency:  write,
			manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
			manifestMIMETypes:    []string{},
			manifestDisclosure:   false,
		}
		if write {
			item[manifestEffect], item[manifestStatusPath], item[manifestDedupWindow] =
				effectWrite, registrationDetailsPath+"operation", agentRegistrationDedupSeconds
		}
		return item
	}
	get := method(
		"get",
		registrationDetailsReadDescription,
		input(map[string]any{}),
		false,
	)
	name := method(
		"set_legal_name",
		"Save the legal name explicitly supplied by the user for registration; "+
			"never copy the Telegram or profile name without explicit intent. "+
			"Read current details version first. Does not register or assign a pass.",
		input(
			map[string]any{
				schemaExpectedVersion: version,
				"legal_name": map[string]any{
					manifestType:    schemaString,
					schemaMinLength: 1,
					schemaMaxLength: detailsLegalNameLimit,
				},
			},
			schemaExpectedVersion,
			"legal_name",
		),
		true,
	)
	passport := method(
		"set_passport_number",
		registrationPassportDescription,
		input(
			map[string]any{
				schemaExpectedVersion: version,
				"passport_number": map[string]any{
					manifestType:    schemaString,
					schemaMinLength: 1,
					schemaMaxLength: detailsPassportLimit,
				},
			},
			schemaExpectedVersion,
			"passport_number",
		),
		true,
	)
	operation := method(
		"operation",
		registrationDetailsOperationDescription,
		input(map[string]any{detailsOperationID: uuidField}, detailsOperationID),
		false,
	)
	return map[string]any{
		"id":                "zinusya_registration_details",
		manifestDescription: "Owner legal registration details, separated from the profile",
		manifestPermissions: []string{},
		manifestDiscovery:   discoveryStatic,
		manifestMethods:     append([]any{get, name, passport, operation}, detailsRequestMethods(method)...),
	}
}

func (s *Server) withRegistrationDetails(modules []any) []any {
	if s.options.RegistrationDetails != nil {
		return append(modules, registrationDetailsModule())
	}
	return modules
}
