package api

const (
	schemaRevocationSubject    = "expected_subject"
	schemaRevocationState      = "expected_revoked"
	schemaRevocationProposal   = "proposal_id"
	schemaUserRevoked          = "revoked"
	revocationSubjectMaxLength = 200
)

func userRevocationMethods() []map[string]any {
	method := administrativeRead(
		"revoke_user",
		"Revoke local account access after reading user and obtaining exact approval of its UUID, "+
			"expected_subject (possibly empty) and expected_revoked. Five-minute approval. "+
			"Does not change remote IAM roles. Each new explicit operation records an audit even if "+
			"already revoked; replay preserves the original receipt. "+
			"Self-revocation immediately prevents further calls by that account; "+
			"another active global administrator can read user_revocation_receipt by proposal_id, but "+
			"cannot confirm the operation. Reconcile the SAME operation after a lost response or 503. "+
			"not_found never proves a safe replacement write. "+
			"Cache invalidation is local to this process.",
		map[string]any{
			schemaUserID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaRevocationSubject: map[string]any{
				manifestType:    schemaString,
				schemaMaxLength: revocationSubjectMaxLength,
			},
			schemaRevocationState: map[string]any{manifestType: schemaBoolean},
		},
		[]string{schemaUserID, schemaRevocationSubject, schemaRevocationState},
		[]string{schemaUserID, "subject", schemaUserRevoked},
	)
	method[manifestEffect] = effectWrite
	method[manifestIdempotency] = true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = userRevocationPath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode: assignmentConfirmationMode, manifestPreparePath: userRevocationPath + "/prepare",
	}
	return []map[string]any{
		method,
		administrativeRead(
			"user",
			"Read only the deployment-scoped user UUID, expected_subject and expected_revoked snapshot "+
				"for local revocation. No profile or directory access.",
			map[string]any{"id": map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}},
			[]string{"id"},
			[]string{schemaUserID, schemaRevocationSubject, schemaRevocationState},
		),
		administrativeRead(
			"user_revocation_receipt",
			"Diagnose a local revocation proposal in this deployment as an active global administrator,"+
				" including after the original actor revoked their own access. "+
				"Returns ORIGINAL actor and operation_id plus result when committed, otherwise no "+
				"result (pending snapshot). Read-only: cannot take over or confirm another actor's "+
				"operation. A pending snapshot does not authorize repeating the write under a new "+
				"operation.",
			map[string]any{
				schemaRevocationProposal: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			},
			[]string{schemaRevocationProposal},
			[]string{schemaRevocationProposal, "actor", "operation_id"},
		),
	}
}
