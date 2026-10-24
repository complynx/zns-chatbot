package api

const orderExpectedCatalogVersion = "expected_catalog_version"
const orderExpectedDraftVersion = "expected_draft_version"

func orderSubmissionMethods() []map[string]any {
	return orderSubmissionMethodsFor(ordersSelectionPath, "selection")
}

func mealSubmissionMethods() []map[string]any {
	return orderSubmissionMethodsFor(mealDraftPath, "meal_draft")
}

func orderSubmissionMethodsFor(path, draftField string) []map[string]any {
	detail := ordersEventProperties()
	detail["order_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	get := paymentMethod(
		"get",
		"Read your current submitted/cancelled order snapshot, including the froz" +
"en exact quote and original sales cutoff. Current entitlement to this ev" +
"ent remains mandatory; other owners and administrative role alone grant " +
"no access.",
		detail,
		[]string{schemaEventID, "order_id"},
		[]string{"id", schemaEventID, schemaVersion, schemaState, "snapshot"},
	)
	get[manifestPath] = path + "get"
	listProps := ordersEventProperties()
	listProps[schemaAfter] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	listProps["limit"] = map[string]any{
		manifestType:  schemaInteger,
		schemaMinimum: 1,
		schemaMaximum: agentTaskPageLimit,
	}
	list := paymentMethod(
		"list",
		"List your retained event orders, including cancelled history. Pages cont" +
"ain at most 20 orders and are also bounded by serialized size; use next " +
"as after until absent, even when fewer than 20 were returned. Current as" +
"signed-pass entitlement and enabled module are required. A saved draft i" +
"s separate from these orders.",
		listProps,
		[]string{schemaEventID},
		[]string{"orders"},
	)
	list[manifestPath] = path + "list"
	props := ordersEventProperties()
	for _, field := range []string{schemaExpectedEventVersion, orderExpectedCatalogVersion, orderExpectedDraftVersion} {
		props[field] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	}
	submitDescription := "Submit your exact nonempty saved draft only after reviewing selection's " +
"complete items and all charges with the user and obtaining exact approva" +
"l of this preparation. Read catalog and selection first; use current eve" +
"nt/catalog/draft versions. Preparation is shared across agent and native" +
" form and expires in five minutes or at the earliest selected offer cuto" +
"ff. The server recalculates and rejects stale versions, changed price/co" +
"ntent, closed sales or expired preparation. Success atomically retains a" +
"n immutable order/receipt and clears only the consumed draft revision. N" +
"o capacity or confirmed money is created; even a zero total does not ass" +
"ert payment. Owner/event/source draft revision is canonical: changing tr" +
"ansport ID cannot duplicate that order. Same operation and exact body/pr" +
"oposal replay the original result after later draft/catalog changes or c" +
"ancellation, subject to current access. Changed arguments conflict. On u" +
"ncertain response reconcile the original operation; not_found is unknown" +
", never permission to replace it."
	if path == mealDraftPath {
		submitDescription = "Submit your exact nonempty saved meal draft after human approval of its " +
"complete frozen composition, all prices and common event-local payment-p" +
"roof deadline. Read catalog and draft first; supply exact event/catalog/" +
"draft revisions. Empty composition creates no order; a selected free dis" +
"h is not empty. A configured common payment deadline and currently open " +
"selected services are mandatory. Preparation lasts at most five minutes " +
"and never beyond the common deadline; execution rechecks selected servic" +
"e sales and current meals entitlement. The bot displays all frozen detai" +
"ls before consent, even when the tool preparation description is abbrevi" +
"ated. Success atomically retains the meal order and original receipt and" +
" clears only this draft revision. Unlimited meal quantities create no ca" +
"pacity holds, confirmed money or evidence. Native and agent calls share " +
"canonical preparations/orders. To continue your own original preparation" +
", use module_call with resume_operation_id and arguments:{} to inspect i" +
"ts exact terms; original expiry and proposal are preserved and fresh exa" +
"ct approval is required. Legacy preparations without a recorded identity" +
" contract cannot be adopted. A new operation cannot duplicate a consumed" +
" draft. Replay returns the original result after later changes/cancellat" +
"ion, subject to current access. Recover the same original operation afte" +
"r uncertainty; not_found is unknown, never authorization to replace it."
	}
	submit := paymentMethod(
		"submit",
		submitDescription,
		props,
		[]string{schemaEventID, schemaExpectedEventVersion, orderExpectedCatalogVersion, orderExpectedDraftVersion},
		[]string{"order", draftField},
	)
	submit[manifestPath] = path + "submit"
	submit[manifestEffect], submit[manifestIdempotency] = effectWrite, true
	submit[manifestStatusPath] = path + "submit/status"
	submit[manifestDedupWindow] = agentRegistrationDedupSeconds
	submit[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: path + "submit/prepare",
	}
	cancelProps := ordersEventProperties()
	cancelProps["order_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	cancelProps[schemaExpectedVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	cancel := paymentMethod(
		"cancel",
		"Cancel only an explicitly requested own order, using its current exact I" +
"D and revision. A new cancellation requires submitted state and must pre" +
"cede the original frozen sales cutoff; catalog edits do not reopen it. T" +
"his stage permits no proof/payment attempt, capacity or money change and" +
" does not restore the draft. Retain history; current authorization is ch" +
"ecked on replay. Same operation/body replays the immutable cancellation " +
"receipt even after cutoff. Missing status is unknown; reconcile the orig" +
"inal operation after uncertainty. Cancel does not select another order o" +
"r resolve an agent dialogue.",
		cancelProps,
		[]string{schemaEventID, "order_id", schemaExpectedVersion},
		[]string{"id", schemaEventID, schemaVersion, schemaState, "snapshot"},
	)
	cancel[manifestPath] = path + "cancel"
	cancel[manifestEffect], cancel[manifestIdempotency] = effectWrite, true
	cancel[manifestStatusPath] = path + "cancel/status"
	cancel[manifestDedupWindow] = agentRegistrationDedupSeconds
	operation := paymentMethod(
		"operation",
		"Recover one exact owner-scoped immutable operation phase: prepare, submi" +
"t or cancel. Prepare is not proof of submission. The receipt's event is " +
"resolved privately and current access is rechecked. not_found is unknown" +
"; do not infer non-execution or create a new operation blindly. Read get" +
" separately for current order state.",
		map[string]any{
			detailsOperationID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaKind: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"prepare", "submit", "cancel"},
			},
		},
		[]string{detailsOperationID, schemaKind},
		[]string{detailsOperationID, schemaEventID, schemaKind, "result"},
	)
	operation[manifestPath] = path + "operation"
	return []map[string]any{list, get, submit, cancel, operation}
}
