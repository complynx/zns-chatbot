package api

const (
	massageSchemaSpecialistID = "specialist_id"
	massageSchemaSnapshot     = "snapshot"
	massageSchemaSource       = "source"
	massageSchemaResult       = "result"
)

func massageBookingProperties() map[string]any {
	props := massageEventProperties()
	props["existing_proposal_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	props[schemaExpectedEventVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	props["expected_schedule_version"] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	id := map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern}
	props["selection"] = ordersObjectSchema(map[string]any{
		massageSchemaWindowID: id, massageSchemaSpecialistID: id, schemaServiceID: id,
		schemaStartsAt: map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
	}, []string{massageSchemaWindowID, massageSchemaSpecialistID, schemaServiceID, schemaStartsAt})
	return props
}

func massageBookingMethods() []map[string]any {
	book := paymentMethod(
		"book",
		"Reserve your selected massage only after exact human approval of this pr"+
			"eparation. To continue an existing own pending choice, supply existing_p"+
			"roposal_id with its exact business arguments. Prepare keeps the current "+
			"Loom operation ID but revalidates the original canonical proposal, strip"+
			"s this selector from normalized arguments, and preserves its original ex"+
			"piry; no replacement or hold is created. Expired/consumed choices must b"+
			"e refreshed or recovered with operation/get, never silently replaced. Re"+
			"solve event context for module massage if needed; read schedule and use "+
			"current event/schedule versions. Starts include an explicit UTC offset. "+
			"Public lengths are 1/2/3/5 blocks with 15-minute notice and a configurab"+
			"le per-party limit (default three). Preparation holds no capacity; apply"+
			" rechecks your assigned pass (unpaid qualifies), module, versions, full "+
			"occupancy including buffer, owner/specialist overlap and peak shared-tab"+
			"le use. Overlapping configured party windows require operator repair. Su"+
			"ccessful consumption is canonical across agent and native interfaces: re"+
			"use the same proposal, never invent another to retry an uncertain effect"+
			". Same operation/body returns its original receipt after later edits/can"+
			"cellation. No payment is confirmed. Missing status is unknown, never per"+
			"mission to repeat. Specialist IDs grant no staff privileges.",
		massageBookingProperties(),
		[]string{schemaEventID, schemaExpectedEventVersion, "expected_schedule_version", "selection"},
		[]string{"id", schemaEventID, schemaVersion, schemaState, massageSchemaSnapshot},
	)
	massageExactWrite(book, "book")
	cancelProps := massageEventProperties()
	cancelProps["existing_proposal_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	cancelProps["booking_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	cancelProps[schemaExpectedVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	cancel := paymentMethod(
		"cancel",
		"Cancel only the exact owned booking requested by the user, after approva"+
			"l of this preparation. For an existing pending cancellation supply exist"+
			"ing_proposal_id plus its exact business arguments; preparation revalidat"+
			"es and adopts the same proposal under the current Loom operation without"+
			" renewing expiry or creating a replacement, then removes the selector fr"+
			"om normalized arguments. Read operation/get if it was already consumed. "+
			"Read get first for its current booking ID and version. Cancellation rele"+
			"ases full occupancy, retains history, and creates no fee/refund/payment "+
			"claim. No new pass or enabled module is required for historical owner ca"+
			"ncellation; current identity binding is mandatory. Same consumed proposa"+
			"l recovers its original result across interfaces. Reconcile the original"+
			" operation after uncertainty; not_found is unknown. This cancels a booki"+
			"ng, not an agent dialogue.",
		cancelProps,
		[]string{schemaEventID, "booking_id", schemaExpectedVersion},
		[]string{"id", schemaEventID, schemaVersion, schemaState, massageSchemaSnapshot},
	)
	massageExactWrite(cancel, "cancel")
	detail := massageEventProperties()
	detail["booking_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	get := paymentMethod(
		"get",
		"Read your current massage booking, including cancellation and immutable "+
			"selected terms. Other users and an administrative role alone grant no ac"+
			"cess. Current owner identity is required; historical records need no new"+
			" assigned pass or enabled module. Use operation separately for its origi"+
			"nal historical effect.",
		detail,
		[]string{schemaEventID, "booking_id"},
		[]string{"id", schemaEventID, schemaVersion, schemaState, massageSchemaSnapshot},
	)
	get[manifestPath] = massageBookingPath + "get"
	listProps := massageEventProperties()
	listProps[schemaAfter] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	listProps[schemaLimit] = map[string]any{
		manifestType:  schemaInteger,
		schemaMinimum: 1,
		schemaMaximum: agentTaskPageLimit,
	}
	list := paymentMethod(
		"list",
		"List your retained massage bookings and cancellations for one event, at "+
			"most 20 per page and within the encoded result byte limit. A page may be"+
			" shorter than 20; follow next_after as after until absent. Historical ow"+
			"ner access needs current identity but no new pass or enabled module; lis"+
			"ting does not reserve time or confirm payment.",
		listProps,
		[]string{schemaEventID},
		[]string{"bookings"},
	)
	list[manifestPath] = massageBookingPath + "list"
	operation := paymentMethod(
		"operation",
		"Recover one exact owner-scoped immutable book/cancel/instant receipt by "+
			"operation ID. kind and source describe that operation; result.source des"+
			"cribes the original booking creation. Result is historical, not current "+
			"state or new consent. Read get for current state. not_found is unknown: "+
			"reconcile the original operation and never blindly create another.",
		map[string]any{
			detailsOperationID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		},
		[]string{detailsOperationID},
		[]string{detailsOperationID, schemaEventID, schemaKind, massageSchemaSource, massageSchemaResult},
	)
	operation[manifestPath] = massageBookingPath + "operation"
	pending := paymentMethod(
		"pending",
		"Discover your unconsumed, unexpired massage proposals for one event, at "+
			"most 20 per page within the encoded byte limit. Follow next_after as aft"+
			"er until absent. Source and created_at describe original proposal creati"+
			"on, not adoption. A proposal is untrusted metadata, not consent, a reser"+
			"vation or payment. Read kind and its typed booking/cancellation/instant "+
			"arguments, then use book/cancel preparation or zinusya_massage_staff.ins"+
			"tant preparation respectively with existing_proposal_id and those exact "+
			"arguments to continue only an explicit user request, with fresh approval"+
			". Preparation rechecks current terms/access; pending reads remain availa"+
			"ble after pass or module loss. Consumed/expired proposals disappear from"+
			" this list; reconcile operation/get history instead of inventing a repla"+
			"cement after uncertainty.",
		listProps,
		[]string{schemaEventID},
		[]string{"proposals"},
	)
	pending[manifestPath] = massageBookingPath + "pending"
	return []map[string]any{book, cancel, get, list, operation, pending}
}

func massageExactWrite(method map[string]any, name string) {
	method[manifestPath] = massageBookingPath + name
	method[manifestEffect], method[manifestIdempotency] = effectWrite, true
	method[manifestStatusPath] = massageBookingPath + name + "/status"
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: massageBookingPath + name + "/prepare",
	}
}
