package api

import "net/http"

// manifest advertises account tools and user-scoped administrative discovery.
const botDescription = "Zinusya (Зинуся) is the zns-chatbot assistant for Zouk Non Stop events. Help users with their explicit requests using authorized tools; answer in the language of the user message."

const (
	schemaPattern         = "pattern"
	schemaBoolean         = "boolean"
	schemaServiceID       = "service_id"
	contentTypeJSON       = "application/json"
	agentPrepared         = "prepared"
	manifestPreparePath   = "prepare_path"
	schemaCurrencyPattern = "^[A-Z]{3}$"
	schemaExternalID      = "external_id"
	schemaPassID          = "pass_id"
	schemaTaskID          = "task_id"
	schemaSizeBytes       = "size_bytes"
	mimeJPEG              = "image/jpeg"
	schemaEvidenceID      = "evidence_id"
	schemaFileID          = "file_id"
	schemaEvidence        = "evidence"
	schemaAmountMinor     = "amount_minor"
	schemaReason          = "reason"
	schemaDateTime        = "date-time"
	schemaMinLength       = "minLength"
	schemaMaxLength       = "maxLength"
	schemaEvents          = "events"
	schemaObservedAt      = "observed_at"
	schemaHasMore         = "has_more"
	schemaChoices         = "choices"
	schemaPriceMinor      = "price_minor"
	schemaCurrency        = "currency"
	schemaCurrencyScale   = "currency_scale"

	agentNotFound         = "not_found"
	schemaExpectedVersion = "expected_version"
	schemaModule          = "module"
	discoveryStatic       = "static"
	manifestMethods       = "methods"

	schemaAfter            = "after"
	schemaLimit            = "limit"
	schemaKind             = "kind"
	schemaRegistrationID   = "registration_id"
	schemaResumeProposalID = "resume_proposal_id"

	schemaMinimum    = "minimum"
	confirmationNone = "none"

	schemaMaximum          = "maximum"
	effectRead             = "read"
	effectWrite            = "write"
	manifestStatusPath     = "status_path"
	manifestDedupWindow    = "deduplication_window_seconds" //nolint:gosec // JSON protocol field, not a credential.
	schemaUserID           = "user_id"
	agentRejected          = "rejected"
	agentTerminalRejection = "terminal_rejection"
	confirmationMode       = "mode"

	agentPending         = "pending"
	agentSucceeded       = "succeeded"
	schemaVersion        = "version"
	schemaState          = "state"
	schemaInteger        = "integer"
	manifestEffect       = "effect"
	manifestMaxResult    = "max_result_bytes"
	manifestInputSchema  = "input_schema"
	manifestConfirmation = "confirmation"
	manifestMIMETypes    = "artifact_mime_types"
	manifestPath         = "path"
	manifestTimeout      = "timeout_ms"
	schemaEventID        = "event_id"
	manifestOutputSchema = "output_schema"
	manifestIdempotency  = "idempotency"
	manifestDisclosure   = "external_disclosure"
	schemaRole           = "role"
	schemaEnum           = "enum"
	manifestDiscovery    = "discovery"
	schemaRequired       = "required"

	schemaUUID                 = "uuid"
	manifestDescription        = "description"
	manifestPermissions        = "permissions"
	schemaObject               = "object"
	schemaString               = "string"
	schemaFormat               = "format"
	schemaAdditionalProperties = "additionalProperties"
	schemaProperties           = "properties"
	manifestType               = "type"
)

const agentContractRevision = "mvp-91"

func (s *Server) manifest(w http.ResponseWriter, _ *http.Request) {
	revision := agentContractRevision
	w.Header().Set("Loom-Operation-Inspect-Path", operationInspectPath)
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol_version":  1,
		schemaServiceID:     "zinusya",
		"revision":          revision,
		manifestDescription: botDescription,
		"caller": map[string]any{
			manifestPermissions: []string{
				"runs.create",
				"runs.control",
				"chat.read",
				"chat.events.append",
				"artifacts.read",
				"artifacts.write",
			},
			"auth": map[string]any{
				manifestType: "zitadel",
				"issuer":     s.options.Issuer,
				"subject":    s.options.ActorID,
				"audience":   s.options.LoomAudience,
			},
		},
		"modules": s.withRegistrationDetails([]any{
			knowledgeModule(),
			profileModule(),
			historyModule(),
			avatarModule(),
			eventContextModule(),
			fileModule(),
			authorityModule(),
			paymentModule(),
			orderEvidenceModule(),
			pairModule(),
			ordersModule(),
			mealsModule(),
			massageModule(),
			massageStaffModule(),
			map[string]any{
				"id":                "zinusya_registrations",
				manifestDescription: "Solo owner registration and assigned-pass reads",
				manifestPermissions: []string{},
				manifestDiscovery:   discoveryStatic,
				manifestMethods:     registrationMethods(),
			},
			map[string]any{
				"id":                "zinusya_administration",
				manifestDescription: "Operator diagnostics, event reads and exact-confirmation pass assignment",
				manifestPermissions: []string{"zinusya.admin"},
				manifestDiscovery:   "dynamic",
				"discovery_path":    "/internal/agent/administration/discover",
				"cache_ttl_seconds": 0,
			},
			map[string]any{
				"id":                "zinusya_account",
				manifestDescription: "Current authenticated account",
				manifestPermissions: []string{},
				manifestDiscovery:   discoveryStatic,
				manifestMethods: []any{
					map[string]any{
						"id":                "me",
						manifestDescription: botDescription + " Read the current authenticated user's account. No user ID argument is accepted.",
						manifestPermissions: []string{},
						manifestPath:        "/internal/agent/account/me",
						manifestEffect:      effectRead,
						manifestTimeout:     toolTimeoutMillis,
						manifestMaxResult:   toolResultLimit,
						manifestInputSchema: map[string]any{
							manifestType:               schemaObject,
							schemaAdditionalProperties: false,
							schemaProperties:           map[string]any{},
						},
						manifestOutputSchema: map[string]any{
							manifestType:               schemaObject,
							schemaAdditionalProperties: false,
							schemaRequired:             []string{"id", "telegram_user_id", "profile"},
							schemaProperties: map[string]any{
								"id": map[string]any{
									manifestType: schemaString,
									schemaFormat: schemaUUID,
								},
								"telegram_user_id": map[string]any{manifestType: schemaInteger},
								"profile":          map[string]any{manifestType: schemaObject},
							},
						},
						manifestIdempotency:  false,
						manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
						manifestMIMETypes:    []string{},
						manifestDisclosure:   false,
					},
				},
			},
		}),
	})
}
