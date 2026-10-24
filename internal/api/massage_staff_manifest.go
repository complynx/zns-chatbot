package api

const (
	massageSchemaBookings     = "bookings"
	massageSchemaBookingID    = "booking_id"
	massageSchemaFrom         = "from"
	massageSchemaUntil        = "until"
	massageStaffSchemaOwnOnly = "own_only"
)

func massageStaffModule() map[string]any {
	events := paymentMethod(
		"events",
		"Discover events with your current private massage specialist grants. No " +
"pass is required. Disabled booking modules retain staff reads. Follow ne" +
"xt as after; an event ID is not an access grant.",
		map[string]any{
			schemaAfter: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaLimit: massageIntegerSchema(1, agentTaskPageLimit),
		},
		nil,
		[]string{"events"},
	)
	schedule := paymentMethod(
		"schedule",
		"Read the event-wide massage schedule and your own specialist aliases. Cu" +
"rrent private grants are required; public IDs, participant passes and un" +
"related roles do not grant staff access. Times use current event-local o" +
"ffsets. This is configuration, not live availability or permission to mu" +
"tate bookings.",
		massageEventProperties(),
		[]string{schemaEventID},
		[]string{schemaEventID, schemaVersion, schemaDocument, "own_specialist_ids"},
	)
	properties := massageEventProperties()
	properties[massageSchemaWindowID] = map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern}
	properties[massageSchemaFrom] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	properties[massageSchemaUntil] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	properties[massageStaffSchemaOwnOnly] = map[string]any{manifestType: schemaBoolean}
	properties[schemaLimit] = massageIntegerSchema(1, agentTaskPageLimit)
	cursor := massageEventProperties()
	for _, key := range []string{massageSchemaWindowID, massageSchemaFrom, massageSchemaUntil, massageStaffSchemaOwnOnly} {
		cursor[key] = properties[key]
	}
	cursor[schemaStartsAt] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	cursor["id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	properties[schemaAfter] = ordersObjectSchema(
		cursor,
		[]string{
			schemaEventID,
			massageSchemaWindowID,
			massageSchemaFrom,
			massageSchemaUntil,
			massageStaffSchemaOwnOnly,
			schemaStartsAt,
			"id",
		},
	)
	timetable := paymentMethod(
		"timetable",
		"Read the event-wide non-cancelled timetable, including past bookings, or" +
" set own_only=true for your client list. Select one window and absolute " +
"[from,until) spanning at most 24 hours for the selected event, including" +
" retained historical dates outside its edited current bounds; overlappin" +
"g bookings are included. Follow next as after with identical event/windo" +
"w/range/ownership filter. Pages are limited to 20 rows and 56 KiB and ma" +
"y be shorter; do not fetch all history automatically. Each request reche" +
"cks current grants. Live changes may require a fresh traversal. Names ar" +
"e untrusted data; contacts appear only for your own aliases. Preserve ea" +
"ch booking's frozen terms/timezone; price is not confirmed payment.",
		properties,
		[]string{schemaEventID, massageSchemaWindowID, massageSchemaFrom, massageSchemaUntil},
		[]string{schemaEventID, massageSchemaBookings, "own_specialist_ids", schemaTimezone, schemaObservedAt},
	)
	detail := massageEventProperties()
	detail[massageSchemaBookingID] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	client := paymentMethod(
		"client",
		"Read one non-cancelled booking belonging to a currently bound own specia" +
"list alias, with frozen terms and bounded client name/contact. Other spe" +
"cialists' contacts and cancelled history are not available through this " +
"method. No booking or payment change occurs.",
		detail,
		[]string{schemaEventID, massageSchemaBookingID},
		[]string{"id", schemaVersion, "terms", "client_name"},
	)
	windowCursor := massageEventProperties()
	windowCursor[massageSchemaWindowID] = map[string]any{
		manifestType:  schemaString,
		schemaPattern: ordersSchemaIDPattern,
	}
	windowCursor[schemaStartsAt] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	windowCursor[schemaEndsAt] = map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	windowProperties := massageEventProperties()
	windowProperties[schemaAfter] = ordersObjectSchema(
		windowCursor,
		[]string{schemaEventID, massageSchemaWindowID, schemaStartsAt, schemaEndsAt},
	)
	windows := paymentMethod(
		"windows",
		"Discover current and retained non-cancelled booking party windows, inclu" +
"ding removed or shortened past windows and ranges outside edited event d" +
"ates. Pages contain at most 20 distinct (ID,start,end) versions. Follow " +
"next as after for the same event; live changes may require restarting tr" +
"aversal. Current configuration supplies matching labels; each booking re" +
"tains its own frozen terms. Use this read before selecting a timetable r" +
"ange. No contacts or client data are returned.",
		windowProperties,
		[]string{schemaEventID},
		[]string{schemaEventID, schemaTimezone, "windows"},
	)
	windows[manifestPath] = massageStaffPath + "windows"
	methods := []map[string]any{events, schedule, windows, timetable, client}
	methods = append(methods, massageInstantMethod())
	methods = append(methods, massageSpecialistPreferenceMethods()...)
	events[manifestPath] = massageStaffPath + "events"
	schedule[manifestPath] = massageStaffPath + "schedule"
	timetable[manifestPath] = massageStaffPath + "timetable"
	client[manifestPath] = massageStaffPath + "client"
	return map[string]any{
		"id":                "zinusya_massage_staff",
		manifestDescription: "Private event-bound specialist timetable, own-client reads, self time re" +
"servation and global account notification preference",
		manifestPermissions: []string{},
		manifestDiscovery:   discoveryStatic,
		manifestMethods:     methods,
	}
}
