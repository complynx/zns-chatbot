package api

import "github.com/complynx/zns-chatbot/internal/registrationdetails"

const detailsPageLimit = 20
const detailsPageResultLimit = 64 << 10
const detailsSalesCloses = "sales_closes_at"
const detailsExpired = "expired"

func detailsObject(properties map[string]any, required ...string) map[string]any {
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

func detailsRequestStatusSchema() map[string]any {
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	version := map[string]any{manifestType: schemaInteger, schemaMinimum: 0}
	request := detailsObject(map[string]any{
		"id":                        map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		schemaEventID:               map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		"event_version":             map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		"event_timezone":            map[string]any{manifestType: schemaString},
		"requirements":              eventDetailsRequirementsSchema(),
		"created_at":                instant,
		"expires_at":                instant,
		"completed_at":              instant,
		"superseded_at":             instant,
		"completed_details_version": version,
	}, "id", schemaEventID, "event_version", "event_timezone", "requirements", "created_at", "expires_at")
	return detailsObject(map[string]any{
		"request": request,
		"state": map[string]any{
			manifestType: schemaString,
			schemaEnum:   []string{invitationPending, "completed", "superseded", detailsExpired},
		},
		"details_version": version,
		"missing_fields":  detailsMissingSchema(),
		schemaObservedAt:  instant,
	}, "request", "state", "details_version", "missing_fields", "observed_at")
}

func detailsMissingSchema() map[string]any {
	return map[string]any{
		manifestType: schemaArray,
		schemaItems: map[string]any{
			manifestType: schemaString,
			schemaEnum:   []string{string(registrationdetails.LegalName), string(registrationdetails.PassportNumber)},
		},
	}
}

func detailsReadinessSchema() map[string]any {
	return detailsObject(map[string]any{
		schemaEventID:      map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		"event_version":    map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		"event_timezone":   map[string]any{manifestType: schemaString},
		detailsSalesCloses: map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
		"details_version":  map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
		"missing_fields":   detailsMissingSchema(), "ready": map[string]any{manifestType: schemaBoolean},
		"observed_at": map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
	}, schemaEventID, "event_version", "event_timezone", detailsSalesCloses, "details_version", "missing_fields", "ready", "observed_at")
}

func detailsRequestMethods(method func(string, string, map[string]any, bool) map[string]any) []any {
	id := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	readiness := method(
		"readiness",
		"Read missing legal fields for an explicit event resolved through zinusya_context. Ready means required fields only; it is not consent, capacity or eligibility for a pass.",
		detailsObject(map[string]any{schemaEventID: id}, schemaEventID),
		false,
	)
	readiness[manifestOutputSchema] = detailsReadinessSchema()
	begin := method(
		"begin_request",
		"Open or reuse a durable data expectation for the selected event and current event version. Does not register, reserve, authorize a continuation or extend an existing deadline. Retain the same operation ID and arguments after an uncertain result.",
		detailsObject(
			map[string]any{
				schemaEventID:              id,
				schemaExpectedEventVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			},
			schemaEventID,
			schemaExpectedEventVersion,
		),
		true,
	)
	begin[manifestStatusPath] = registrationDetailsPath + "request_operation"
	begin[manifestOutputSchema] = detailsRequestStatusSchema()
	read := method(
		"request",
		"Read a current owner-only data expectation snapshot. Recheck current event/readiness before continuing; completion is not registration or consent.",
		detailsObject(map[string]any{"request_id": id}, "request_id"),
		false,
	)
	read[manifestOutputSchema] = detailsRequestStatusSchema()
	operation := method(
		"request_operation",
		"Recover the immutable redacted begin_request receipt using arguments.operation_id or the original envelope operation_id on the status endpoint. Missing status is unknown: keep the same key and arguments.",
		detailsObject(map[string]any{detailsOperationID: id}, detailsOperationID),
		false,
	)
	operation[manifestOutputSchema] = detailsRequestStatusSchema()
	list := method(
		"requests",
		"List up to 20 nonsuperseded owner expectations, including completed or expired ones, ordered by request UUID. Follow next until absent; snapshots are context, never consent.",
		detailsObject(
			map[string]any{
				"after": id,
				"limit": map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: detailsPageLimit},
			},
		),
		false,
	)
	list[manifestOutputSchema] = detailsObject(
		map[string]any{
			"requests": map[string]any{
				manifestType:         schemaArray,
				schemaItems:          detailsRequestStatusSchema(),
				ordersSchemaMaxItems: detailsPageLimit,
			},
			schemaNext: id,
		},
		"requests",
	)
	list[manifestMaxResult] = detailsPageResultLimit
	return []any{readiness, begin, read, operation, list}
}
