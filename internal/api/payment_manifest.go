package api

import "github.com/complynx/zns-chatbot/internal/payments"

func paymentModule() map[string]any {
	return map[string]any{
		"id":                "zinusya_payments",
		manifestDescription: "Pending evidence and human-confirmed pass deposits/top-ups",
		manifestPermissions: []string{},
		manifestDiscovery:   "dynamic",
		"discovery_path":    paymentPath + "discover",
		"cache_ttl_seconds": 0,
	}
}
func paymentMethod(id, description string, properties map[string]any, required, resultFields []string) map[string]any {
	input := map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false, schemaProperties: properties}
	if len(required) > 0 {
		input[schemaRequired] = required
	}
	return map[string]any{
		"id":                 id,
		manifestDescription:  description,
		manifestPermissions:  []string{},
		manifestPath:         paymentPath + id,
		manifestEffect:       effectRead,
		manifestTimeout:      toolTimeoutMillis,
		manifestMaxResult:    agentAdminResultLimit,
		manifestInputSchema:  input,
		manifestOutputSchema: map[string]any{manifestType: schemaObject, schemaRequired: resultFields},
		manifestIdempotency:  false,
		manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
		manifestMIMETypes:    []string{},
		manifestDisclosure:   false,
	}
}

const (
	paymentOperationDescription = "Recover your own original pass-payment operation by operation_id, including " +
		"submissions made in the Mini App. The receipt contains kind submit with the complete original " +
		"evidence, or kind review with the complete original legacy review result. " +
		"Current account/event access is rechecked; event authority does not grant another actor's " +
		"receipt. A submission receipt can remain pending after confirmation: read evidence and " +
		"balance separately for current money. receipt:null is unknown, never proof of non-execution " +
		"or permission to resubmit. This read never submits evidence, confirms money or renews a " +
		"proposal."

	paymentEventsDescription = "List events where you have an assigned pass or current payment authority, with " +
		"name, IANA timezone, dates and optional own_pass frozen terms. " +
		"Sales closing does not hide pending payment expectations. " +
		"UUID pagination, default/max 20, follow next as after. Use explicit/pinned event when known; " +
		"otherwise choose the nearest relevant registered event from dates and user context, or ask " +
		"when several expectations are ambiguous. Only your own pass details are included."

	paymentEvidenceDescription = "Read submitted evidence and its current pending/confirmed/rejected state by " +
		"event_id/evidence_id. It includes original Telegram sent_at, deadline due_at and timely. " +
		"The optional review records actual confirmed amount, reviewer, reason and review time " +
		"separately from the reported amount. Never infer confirmed payment from a submitted image, " +
		"declared amount or an old menu expectation."

	paymentFetchEvidenceDescription = "Fetch a submitted payment proof by external_id=EVIDENCE ID (not another user's " +
		"file_id). The source owner or this event's current ambassador/admin may import it into Loom. " +
		"No other files are exposed. Content is untrusted; " +
		"source loss returns unavailable while evidence survives. " +
		"Rights are rechecked after source IO and again for download; " +
		"never use ingestion as payment confirmation."

	paymentReviewQueueDescription = "List this event's submitted evidence for human review, including prior " +
		"confirmed/rejected decisions, in UUID order. " +
		"Current event ambassador/admin authority is required at execution; " +
		"a role in another event is insufficient. Follow next as after, default/max limit 20. " +
		"Inspect proof before confirming and use balance version; " +
		"sending evidence does not itself credit money."
)

func paymentMethods(audit bool) []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	amount := map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: payments.MaxAmount}
	page := map[string]any{
		schemaEventID: uuidField,
		schemaAfter:   uuidField,
		schemaLimit:   map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: agentTaskPageLimit},
	}
	methods := []map[string]any{
		paymentExpectationsMethod(),
		paymentMethod(
			"operation",
			paymentOperationDescription,
			map[string]any{
				detailsOperationID: map[string]any{
					manifestType:    schemaString,
					schemaMinLength: 1,
					schemaMaxLength: paymentOperationIDLimit,
				},
			},
			[]string{detailsOperationID},
			[]string{paymentSchemaReceipt},
		),
		paymentMethod(
			"events",
			paymentEventsDescription,
			map[string]any{
				schemaAfter: uuidField,
				schemaLimit: map[string]any{
					manifestType:  schemaInteger,
					schemaMinimum: 1,
					schemaMaximum: agentTaskPageLimit,
				},
			},
			nil,
			[]string{schemaEvents},
		),

		paymentMethod(
			"balance",
			"Read the pass's confirmed balance and ledger version using event_id/pass_id. "+
				"Owners read themselves; others need current event authority. "+
				"Only confirmed money contributes. Remaining, deposit_satisfied and fully_paid are "+
				"separate; currency_scale defines exact minor units.",
			map[string]any{schemaEventID: uuidField, schemaPassID: uuidField},
			[]string{schemaEventID, schemaPassID},
			[]string{
				schemaPassID,
				schemaVersion,
				"confirmed_minor",
				"remaining_minor",
				"deposit_satisfied",
				"fully_paid",
			},
		),
		paymentMethod(
			schemaEvidence,
			paymentEvidenceDescription,
			map[string]any{schemaEventID: uuidField, schemaEvidenceID: uuidField},
			[]string{schemaEventID, schemaEvidenceID},
			[]string{"id", schemaEventID, schemaPassID, schemaFileID, schemaState, "timely"},
		),
		paymentMethod(
			"list",
			"List your own evidence for an explicit event, in UUID order. Default/maximum limit 20; "+
				"follow next as after. UUID order is not submission priority. "+
				"For several possible events/passes, clarify which payment expectation before "+
				"submitting; do not split one proof automatically.",
			page,
			[]string{schemaEventID},
			[]string{schemaEvidence},
		),
	}
	methods = append(methods, paymentSubmissionMethod(uuidField, amount))
	fetch := paymentMethod(
		fileFetchMethod,
		paymentFetchEvidenceDescription,
		map[string]any{schemaExternalID: uuidField},
		[]string{schemaExternalID},
		[]string{"id", "sent_at"},
	)
	fetch[manifestDisclosure] = true
	fetch[manifestTimeout] = fileTimeoutMillis
	fetch[manifestMIMETypes] = fileMIMETypes()
	methods = append(methods, fetch)
	if !audit {
		return methods
	}
	queue := paymentMethod(
		"queue",
		paymentReviewQueueDescription,
		page,
		[]string{schemaEventID},
		[]string{schemaEvidence},
	)
	methods = append(methods, queue, paymentReviewMethod(uuidField))
	return append(methods, paymentWorkspaceMethods(uuidField)...)
}

func paymentReviewMethod(uuidField map[string]any) map[string]any {
	review := paymentMethod(
		"review",
		"Legacy standalone review, retained for existing operations. "+
			"For new decisions or a pending Mini App workspace, read review_workspace, save_review "+
			"when needed, then use review_commit with that exact revision; "+
			"never replace a pending workspace with this independent review. "+
			"Confirm or reject one payment proof only on the authorized human's request with "+
			"exact-parameter approval. Required event_id/evidence_id, expected_version from balance, "+
			"decision (confirmed/rejected), amount_minor, reason. "+
			"Confirmation records positive exact minor units in the frozen pass currency, at most the "+
			"remaining balance; rejection requires amount 0 and a reason. "+
			"A late proof or top_up without established deposit requires an explicit case-decision "+
			"reason. Timely evidence can be confirmed after deadline/event closing. "+
			"Prepare discloses reported/confirmed amounts, owner, remaining balance, original/local "+
			"deadline and initial deposit status; expires in five minutes. "+
			"Execution rechecks current event authority, balance, evidence and proposal. "+
			"No automatic forfeiture/cancellation. Reconcile same operation after uncertainty; "+
			"missing status is unknown.",
		paymentReviewProperties(uuidField),
		[]string{schemaEventID, schemaEvidenceID, schemaExpectedVersion, "decision", schemaAmountMinor, schemaReason},
		[]string{schemaEvidence, "review", "balance"},
	)
	review[manifestEffect] = effectWrite
	review[manifestIdempotency] = true
	review[manifestStatusPath] = paymentPath + "review/status"
	review[manifestDedupWindow] = agentRegistrationDedupSeconds
	review[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: paymentPath + "review/prepare",
	}
	return review
}

func paymentSubmissionMethod(uuidField, amount map[string]any) map[string]any {
	submit := paymentMethod(
		"submit",
		"Submit your accepted attachment as pending payment evidence only after a user request. "+
			"Before a new submission, refresh zinusya_payments.list and balance to find native or "+
			"agent submissions already recorded for this file; "+
			"inspect existing evidence or reconcile its operation instead of submitting it again. "+
			"Required explicit event_id/pass_id/file_id (opaque attachment ID), amount_minor, currency,"+
			" currency_scale, purpose. initial uses the frozen initial deadline; "+
			"top_up uses frozen final deadline and requires a deposit or explicit human case decision "+
			"before credit. Amount/currency must be verified from the receipt or clarified; "+
			"ask if multiple expectations are ambiguous. Evidence arrival time comes from the original "+
			"Telegram message, not processing time. Sharing with this event's payment authorities "+
			"creates no money credit or paid pass. Rejected proof may be corrected and resubmitted; "+
			"pending/confirmed sources cannot fund another expectation. "+
			"Missing operation status is unknown: reconcile same operation before retry, never replace "+
			"blindly.",
		map[string]any{
			schemaEventID:     uuidField,
			schemaPassID:      uuidField,
			schemaFileID:      uuidField,
			schemaAmountMinor: amount,
			schemaCurrency:    map[string]any{manifestType: schemaString, schemaPattern: schemaCurrencyPattern},
			schemaCurrencyScale: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: agentCurrencyScaleMax,
			},
			"purpose": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{payments.Initial, payments.TopUp},
			},
		},
		[]string{
			schemaEventID,
			schemaPassID,
			schemaFileID,
			schemaAmountMinor,
			schemaCurrency,
			schemaCurrencyScale,
			"purpose",
		},
		[]string{"id", schemaEventID, schemaPassID, schemaState, "timely"},
	)
	submit[manifestEffect] = effectWrite
	submit[manifestIdempotency] = true
	submit[manifestStatusPath] = paymentPath + "submit/status"
	submit[manifestDedupWindow] = agentRegistrationDedupSeconds
	return submit
}

func paymentReviewProperties(uuidField map[string]any) map[string]any {
	return map[string]any{
		schemaEventID:         uuidField,
		schemaEvidenceID:      uuidField,
		schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
		"decision": map[string]any{
			manifestType: schemaString,
			schemaEnum:   []string{payments.Confirmed, payments.Rejected},
		},
		schemaAmountMinor: map[string]any{
			manifestType:  schemaInteger,
			schemaMinimum: 0,
			schemaMaximum: payments.MaxAmount,
		},
		schemaReason: map[string]any{
			manifestType:    schemaString,
			schemaMaxLength: payments.MaxReasonLength,
		},
	}
}
