package api

const (
	paymentSchemaWorkspaceID = "workspace_id"
	paymentOperationIDLimit  = 128
	paymentSchemaReview      = "review"
	paymentSchemaDecision    = "decision"
	paymentSchemaRevision    = "revision"
	paymentSchemaReceipt     = "receipt"
)

func paymentWorkspaceMethods(uuidField map[string]any) []map[string]any {
	reviewSchema := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaProperties: paymentReviewProperties(
			uuidField,
		),
		schemaRequired: []string{
			schemaEventID,
			schemaEvidenceID,
			schemaExpectedVersion,
			paymentSchemaDecision,
			schemaAmountMinor,
			schemaReason,
		},
	}
	revision := map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	workspace := paymentMethod(
		"review_workspace",
		"Read your latest prepared payment decision for event_id/evidence_id before continuing a native form or agent review. workspace:null means no saved decision was found, not a payment result. It contains exact arguments, canonical proposal, expiry, revision, state and current flag. Saving intent never credits money. Inspect current evidence and balance as well; another reviewer may already have decided it.",
		map[string]any{
			schemaEventID:    uuidField,
			schemaEvidenceID: uuidField,
		},
		[]string{schemaEventID, schemaEvidenceID},
		[]string{"workspace"},
	)
	save := paymentMethod(
		"save_review",
		"Save an explicit human-requested review intention without crediting money or treating it as approval. Read review_workspace, evidence and balance first. Supply the full review and expected_revision (0 only for no previous workspace). An edit creates a new immutable workspace ID/revision and invalidates old pending confirmations. Reject stale revisions rather than silently overwriting. Retrying the same operation returns its original snapshot; read current state again after replay. After saving, obtain exact human approval through review_commit, or allow the native form to continue the same decision.",
		map[string]any{
			paymentSchemaReview: reviewSchema,
			"expected_revision": map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
		},
		[]string{paymentSchemaReview, "expected_revision"},
		[]string{paymentSchemaWorkspaceID, paymentSchemaRevision, schemaState, "preparation"},
	)
	save[manifestEffect], save[manifestIdempotency] = effectWrite, true
	save[manifestStatusPath] = paymentPath + "save_review/status"
	save[manifestDedupWindow] = agentRegistrationDedupSeconds
	cancel := paymentMethod(
		"cancel_review",
		"Cancel only this actor's current pending workspace when the user asks to stop that review. Required workspace_id/revision. It invalidates its pending confirmations but never cancels a pass, rejects a proof or reverses money. Already completed money cannot be cancelled here. No text tags are required. Reconcile the same operation after an uncertain response; do not create replacement decisions without new user intent.",
		map[string]any{
			paymentSchemaWorkspaceID: uuidField,
			paymentSchemaRevision:    revision,
		},
		[]string{paymentSchemaWorkspaceID, paymentSchemaRevision},
		[]string{paymentSchemaWorkspaceID, paymentSchemaRevision, schemaState, "preparation"},
	)
	cancel[manifestEffect], cancel[manifestIdempotency] = effectWrite, true
	cancel[manifestStatusPath] = paymentPath + "cancel_review/status"
	cancel[manifestDedupWindow] = agentRegistrationDedupSeconds
	commit := paymentMethod(
		"review_commit",
		"Execute a saved exact payment review only after explicit human exact-parameter approval. Read the workspace and pass its workspace_id/revision plus the full unchanged review (event/evidence, balance version, decision, exact credited minor amount and reason). Preparation discloses frozen currency/scale, reported versus credited amount, remaining balance, local deadline and reason; its expiry is the original workspace expiry and retries never renew it. Authority, current revision, balance, evidence and expiry are checked again. Native completion and this call share one canonical operation; replay or another interface's prior completion returns the original canonical receipt without new credit. The transport operation ID is distinct from canonical_operation_id. After uncertainty reconcile the same transport operation; missing status is unknown. To change parameters save a new revision and obtain fresh approval.",
		map[string]any{
			paymentSchemaWorkspaceID: uuidField,
			paymentSchemaRevision:    revision,
			paymentSchemaReview:      reviewSchema,
		},
		[]string{paymentSchemaWorkspaceID, paymentSchemaRevision, paymentSchemaReview},
		[]string{"canonical_operation_id", paymentSchemaReview},
	)
	commit[manifestEffect], commit[manifestIdempotency] = effectWrite, true
	commit[manifestStatusPath] = paymentPath + "review_commit/status"
	commit[manifestDedupWindow] = agentRegistrationDedupSeconds
	commit[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: paymentPath + "review_commit/prepare",
	}
	operation := paymentMethod(
		"review_operation",
		"Read an original workspace transport operation by operation_id only. receipt:null is unknown, not proof of non-execution. A pending commit returns its original exact preparation and workspace, with unavailable_reason when cancelled, superseded or expired. A canonical result completed through another approved interface is recovered read-only. A completed prepare/cancel receipt is only intent/history and never means paid; only a commit review result records confirmed/rejected money. Current authority is required for all returned data.",
		map[string]any{
			detailsOperationID: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: paymentOperationIDLimit,
			},
		},
		[]string{detailsOperationID},
		[]string{paymentSchemaReceipt},
	)
	return []map[string]any{workspace, save, cancel, commit, operation}
}
