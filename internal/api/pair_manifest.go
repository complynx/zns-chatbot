package api

const (
	schemaStartsAt             = "starts_at"
	schemaPair                 = "pair"
	schemaTimezone             = "timezone"
	schemaArray                = "array"
	schemaItems                = "items"
	schemaEndsAt               = "ends_at"
	pairAssignMethod           = "assign"
	schemaPairID               = "pair_id"
	schemaExpectedEventVersion = "expected_event_version"
	discoveryDynamic           = "dynamic"
	manifestDiscoveryPath      = "discovery_path"
	manifestCacheTTL           = "cache_ttl_seconds"
	schemaPairs                = "pairs"
	schemaCreatedAt            = "created_at"
	schemaAssignedAt           = "assigned_at"
)

func pairModule() map[string]any {
	return map[string]any{
		"id":                  "zinusya_pairs",
		manifestDescription:   "Participant pair metadata and uncoupling, with exact administrator pass assignment. Each owner retains an individual pass and payment history.",
		manifestPermissions:   []string{},
		manifestDiscovery:     discoveryDynamic,
		manifestDiscoveryPath: "/internal/agent/pairs/discover",
		manifestCacheTTL:      0,
	}
}

func pairParticipantMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	my := paymentMethod(
		"my",
		"Read your active pair metadata, with event name, dates and IANA timezone. Optional event_id filters one event. UUID pagination, default/max20; follow next as after. Closed or disabled events remain visible for cleanup. No partner profile, pass or payment data. Read this before choosing a pair to dissolve; ask when the intended event or pair is ambiguous.",
		map[string]any{
			schemaEventID: uuidField,
			schemaAfter:   uuidField,
			schemaLimit: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
				schemaMaximum: agentTaskPageLimit,
			},
		},
		nil,
		[]string{schemaPairs},
	)
	my[manifestPath] = "/internal/agent/pairs/my"
	my[manifestOutputSchema] = ownedPairsSchema()
	dissolve := paymentMethod(
		"dissolve",
		"End only your pair relationship after an explicit participant request. Required event_id, pair_id and expected_version come from my. Either participant may end it; administrator rights do not impersonate consent. Closing, disabled modules or a revoked counterpart do not prevent cleanup. Individual passes, price/deposit terms and payment evidence or confirmed money remain intact. This does not cancel a pass or refund money. Reconcile the same operation ID after uncertainty; missing status is unknown, never replace the operation blindly.",
		map[string]any{
			schemaEventID:         uuidField,
			schemaPairID:          uuidField,
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		},
		[]string{schemaEventID, schemaPairID, schemaExpectedVersion},
		nil,
	)
	dissolve[manifestPath] = pairDissolutionPath
	dissolve[manifestEffect] = effectWrite
	dissolve[manifestOutputSchema] = pairSchema()
	dissolve[manifestIdempotency] = true
	dissolve[manifestStatusPath] = pairDissolutionPath + "/status"
	dissolve[manifestDedupWindow] = agentRegistrationDedupSeconds
	return []map[string]any{my, dissolve}
}

func ownedPairsSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	return map[string]any{
		manifestType: schemaObject, schemaAdditionalProperties: false, schemaRequired: []string{schemaPairs},
		schemaProperties: map[string]any{
			"next": uuidField,
			schemaPairs: map[string]any{
				manifestType: schemaArray,
				schemaItems: map[string]any{
					manifestType: schemaObject, schemaAdditionalProperties: false,
					schemaRequired: []string{schemaPair, "event_name", schemaTimezone, schemaStartsAt, schemaEndsAt},
					schemaProperties: map[string]any{
						schemaPair:     pairSchema(),
						"event_name":   map[string]any{manifestType: schemaString},
						schemaTimezone: map[string]any{manifestType: schemaString},
						schemaStartsAt: instant, schemaEndsAt: instant,
					},
				},
			},
		},
	}
}

func pairAssignmentMethod() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	version := map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	return map[string]any{
		"id":                pairAssignMethod,
		manifestDescription: "Assign both passes atomically only after a global or event administrator request and exact approval. Read event, pair and both registration versions first. Preparation names both recipients, each configured price/currency, deposit minimum and event-local payment deadlines; it reserves no capacity. Confirmation lasts at most five minutes. Both accounts, membership, versions and capacities are rechecked before writing. No payment is recorded. After uncertainty reconcile the same operation ID; missing status is unknown, never create a replacement operation blindly.",
		manifestPermissions: []string{},
		manifestPath:        pairAssignmentPath,
		manifestEffect:      effectWrite,
		manifestTimeout:     toolTimeoutMillis,
		manifestMaxResult:   agentAdminResultLimit,
		manifestInputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired: []string{
				schemaEventID,
				schemaPairID,
				schemaExpectedEventVersion,
				"expected_pair_version",
				"expected_leader_version",
				"expected_follower_version",
			},
			schemaProperties: map[string]any{
				schemaEventID: uuidField, schemaPairID: uuidField,
				schemaExpectedEventVersion: version, "expected_pair_version": version,
				"expected_leader_version": version, "expected_follower_version": version,
			},
		},
		manifestOutputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaRequired:             []string{schemaPair, "leader", "follower"},
			schemaAdditionalProperties: false,
			schemaProperties: map[string]any{
				schemaPair: pairSchema(),
				"leader":   registrationSchema(), "follower": registrationSchema(),
			},
		},
		manifestIdempotency: true,
		manifestStatusPath:  pairAssignmentPath + "/status",
		manifestDedupWindow: agentRegistrationDedupSeconds,
		manifestConfirmation: map[string]any{
			confirmationMode:    assignmentConfirmationMode,
			manifestPreparePath: pairAssignmentPath + "/prepare",
		},
		manifestMIMETypes:  []string{},
		manifestDisclosure: false,
	}
}

func pairSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			"id",
			schemaEventID,
			"leader_registration_id",
			"follower_registration_id",
			schemaState,
			schemaVersion,
			"queued_at",
			schemaCreatedAt,
		},
		schemaProperties: map[string]any{
			"id":                       uuidField,
			schemaEventID:              uuidField,
			"leader_registration_id":   uuidField,
			"follower_registration_id": uuidField,
			schemaState: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"waitlisted", "assigned", "dissolved"},
			},
			schemaVersion:    map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			"queued_at":      instant,
			schemaCreatedAt:  instant,
			schemaAssignedAt: instant,
			"dissolved_at":   instant,
		},
	}
}

func pairPrerequisiteMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	page := map[string]any{
		schemaAfter: uuidField,
		schemaLimit: map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: agentTaskPageLimit},
	}
	events := paymentMethod(
		schemaEvents,
		"List public event metadata where you currently have pair assignment authority. UUID pagination, default/max20; follow next as after. No participant pass or payment records.",
		page,
		nil,
		[]string{schemaEvents},
	)
	events[manifestPath] = "/internal/agent/pairs/events"
	candidates := paymentMethod(
		"candidates",
		"Read waitlisted pair IDs, both participant account IDs and all current versions needed for exact assignment in an authorized event. UUID pagination is not allocation priority. Copy the returned arguments to assign preparation. No capacity reservation or payment data; preparation rechecks eligibility.",
		map[string]any{
			schemaEventID: uuidField,
			schemaAfter:   uuidField,
			schemaLimit:   page[schemaLimit],
		},
		[]string{schemaEventID},
		[]string{"candidates"},
	)
	candidates[manifestPath] = "/internal/agent/pairs/candidates"
	return []map[string]any{events, candidates}
}
