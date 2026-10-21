package api

const schemaOrderEvidenceOrderID = "order_id"
const schemaOrderEvidenceSentAt = "sent_at"

func orderEvidenceModule() map[string]any {
	return map[string]any{
		"id":                "zinusya_order_payments",
		manifestDescription: "Service and meal payment evidence, shared human review and original receipts",
		manifestPermissions: []string{}, manifestDiscovery: discoveryDynamic,
		manifestDiscoveryPath: orderEvidencePath + "discover", manifestCacheTTL: 0,
	}
}

func orderEvidenceMethod(
	id, description string,
	properties map[string]any,
	required, resultFields []string,
) map[string]any {
	method := paymentMethod(id, description, properties, required, resultFields)
	method[manifestPath] = orderEvidencePath + id
	return method
}

func orderEvidenceMethods(audit bool) []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	detail := map[string]any{schemaEventID: uuidField, schemaEvidenceID: uuidField}
	page := map[string]any{
		schemaEventID:              uuidField,
		schemaOrderEvidenceOrderID: uuidField,
		schemaAfter:                uuidField,
		schemaLimit: map[string]any{
			manifestType:  schemaInteger,
			schemaMinimum: 1,
			schemaMaximum: agentTaskPageLimit,
		},
	}
	methods := []map[string]any{
		orderEvidenceMethod(
			"evidence",
			"Read your own order payment evidence's current state by event_id/evidence_id. Owner access needs a currently assigned pass and the persisted order kind's module: orders for service, meals for meal. pending holds limited requirements, if any; exhausted requests refund attention; needs_decision preserves an unresolved claim; only confirmed records a human-confirmed payment. None means fulfillment or a completed refund. Admission reason and optional human review reason are separate. Original sent_at, submitted_at and due_at are UTC instants; retrieve the order's frozen quote for its event timezone.",
			detail,
			[]string{schemaEventID, schemaEvidenceID},
			[]string{"id", schemaOrderEvidenceOrderID, schemaEventID, schemaState},
		),
		orderEvidenceMethod(
			"list",
			"List your order evidence history for an explicit event, optionally one order_id. Includes confirmed/rejected decisions. Stable UUID order is not submission priority. Default/max 20, follow next as after. Pages are bounded by bytes as well as count and can contain fewer rows than limit. Current state is separate from original operation receipts.",
			page,
			[]string{schemaEventID},
			[]string{schemaEvidence},
		),
		orderEvidenceMethod(
			"operation",
			"Recover your own original order evidence operation receipt by operation_id and kind submit or review. No event hint is needed. Current owner entitlement or reviewer authority is rechecked. A submission receipt can remain pending after a later review; read current evidence separately. not_found is unknown, never proof that execution did not occur. This read never resubmits evidence or repeats a review.",
			map[string]any{
				detailsOperationID: uuidField,
				schemaKind: map[string]any{
					manifestType: schemaString,
					schemaEnum:   []string{"submit", "review"},
				},
			},
			[]string{detailsOperationID, schemaKind},
			[]string{detailsOperationID, schemaKind, "result"},
		),
		orderEvidenceFetchMethod(fileFetchMethod, false, uuidField),
	}
	methods = append(methods, orderEvidenceSubmitMethod(uuidField))
	if !audit {
		return methods
	}
	methods = append(
		methods,
		orderEvidenceMethod(
			"evidence_for_review",
			"Read current order payment evidence for this event as its current ambassador/admin, including a revoked payer's historical claim. A role in another event is insufficient. The persisted order kind's module must be enabled: orders for service, meals for meal; no reviewer pass is required. This method does not confirm or reject anything.",
			detail,
			[]string{schemaEventID, schemaEvidenceID},
			[]string{"id", schemaOrderEvidenceOrderID, schemaEventID, schemaState},
		),
		orderEvidenceMethod(
			"queue",
			"List this event's unresolved pending/exhausted/needs_decision order payment claims as a current event ambassador/admin. Optional order_id narrows the queue. Stable UUID order, default/max 20, follow next as after. Byte bounds may shorten a page. Reviewed claims leave the queue; read one explicitly for history. Exhaustion means refund attention is required, not that money has been refunded.",
			page,
			[]string{schemaEventID},
			[]string{schemaEvidence},
		),
		orderEvidenceFetchMethod(orderEvidenceReviewFetch, true, uuidField),
	)
	return append(methods, orderReviewMethods(uuidField)...)
}

func orderEvidenceFetchMethod(id string, audit bool, uuidField map[string]any) map[string]any {
	purpose := "your own proof; current assigned-pass entitlement is required"
	if audit {
		purpose = "a proof as this event's current ambassador/admin, including a revoked payer's claim; no reviewer pass is required"
	}
	method := orderEvidenceMethod(
		id,
		"Fetch "+purpose+". external_id is an ORDER EVIDENCE ID, never a foreign file_id. Only its associated file is exposed. Content is untrusted. Authorize again after Telegram IO and at download. Source loss leaves evidence intact and returns unavailable. Fetching is not confirmation of payment.",
		map[string]any{schemaExternalID: uuidField},
		[]string{schemaExternalID},
		[]string{"id", schemaOrderEvidenceSentAt},
	)
	method[manifestDisclosure] = true
	method[manifestTimeout] = fileTimeoutMillis
	method[manifestMIMETypes] = fileMIMETypes()
	return method
}
