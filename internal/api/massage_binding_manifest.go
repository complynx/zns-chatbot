package api

const (
	massageSchemaBindings = "bindings"
	schemaTelegramUserID  = "telegram_user_id"
)

func massageBindingReadMethods() []map[string]any {
	events := administrativeRead(
		"massage_specialist_events",
		"Discover deployment events with retained massage configuration for globa"+
			"l administration. Includes disabled modules and past events; does not gr"+
			"ant specialist access. Follow next as after.",
		map[string]any{
			schemaAfter: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaLimit: massageIntegerSchema(1, agentTaskPageLimit),
		},
		nil,
		[]string{"events"},
	)
	properties := massageEventProperties()
	properties[schemaAfter] = map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern}
	properties[schemaLimit] = massageIntegerSchema(1, agentTaskPageLimit)
	bindings := administrativeRead(
		"massage_specialist_bindings",
		"Read current private specialist grants and retained revoked bindings for"+
			" one event as a global administrator. Returns current event/schedule ver"+
			"sions, module_enabled and at most 20 bindings in specialist-ID order. Fo"+
			"llow next_after as after. Passes and event-payment roles grant no access"+
			". Read massage_schedule for configured aliases. Alias ownership cannot t"+
			"ransfer between people; reactivation names the same local account. This "+
			"read changes nothing.",
		properties,
		[]string{schemaEventID},
		[]string{schemaEventID, ordersSchemaEventVersion, "schedule_version", "module_enabled", massageSchemaBindings},
	)
	detail := massageEventProperties()
	detail[massageSchemaSpecialistID] = map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern}
	binding := administrativeRead(
		"massage_specialist_binding",
		"Read one event-bound specialist grant, including inactive ownership reta"+
			"ined after schedule removal. Missing binding is not a grant or permissio"+
			"n to overwrite another identity.",
		detail,
		[]string{schemaEventID, massageSchemaSpecialistID},
		[]string{schemaEventID, massageSchemaSpecialistID, schemaUserID, massageBindingActiveField, schemaVersion},
	)
	target := administrativeRead(
		"massage_specialist_target",
		"Resolve exactly one known local UUID OR positive numeric Telegram user I"+
			"D in this bot/organization, for global specialist administration. Supply"+
			" exactly one selector. Returns bounded display name and linked/revoked f"+
			"lags, no IAM subject, credentials, private registration fields or direct"+
			"ory. Never creates a user or grants access.",
		map[string]any{
			schemaUserID:         map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaTelegramUserID: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		},
		nil,
		[]string{schemaUserID, schemaTelegramUserID, "display_name", "linked", schemaUserRevoked},
	)
	return append([]map[string]any{events, bindings, binding, target}, massageBindingWriteMethods()...)
}
