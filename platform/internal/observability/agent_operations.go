package observability

// The finite public vocabulary is a privacy boundary, not a namespace filter.
func safeAgentOperation(operation string) string {
	switch operation {
	case "orders.browse", "orders.choice", "orders.contacts",
		"orders.event", "orders.events", "orders.export",
		"orders.history.page", "orders.history.read", "orders.inbox",
		"orders.inspect", "orders.instructions", "orders.proof",
		"orders.quote", "orders.review.decide", "orders.review.proof",
		"orders.review.read", "orders.update",
		"diagnostic", "telegram.update", "model.plan",
		"model.skills", "model.knowledge_assessment", "model.history_summary",
		"model.broadcast_name", "js.run", "discovery",
		"read", "write", "search",
		"workflow.get", "workflow.catalog", "workflow.select",
		"orders.list", "orders.get", "orders.change",
		"food.view", "food.quote", "food.change",
		"food.payment.prepare", "food.review.queue", "food.review.read",
		"food.review.decide", "food.review.proof", "food.export",
		"orders.page", "orders.read", "history.page", "lineup.query",
		"passes.events", "passes.get", "passes.invitations",
		"passes.event.read", "passes.registration.read", "passes.registration.show",
		"passes.admin.queue", "passes.admin.target", "passes.payments.review",
		"passes.takeover.read", "passes.tiers", "passes.export",
		"passes.resume", "passes.operations", "passes.batch.assign",
		"passes.batch.cancel", "passes.batch.uncouple", "passes.registration.solo",
		"passes.registration.invite", "passes.registration.accept", "passes.registration.decline",
		"passes.registration.cancel", "passes.registration.payment_admin", "passes.admin.assign",
		"passes.admin.cancel", "passes.admin.uncouple", "passes.admin.recalculate",
		"passes.payments.accept", "passes.payments.reject", "passes.takeover.apply",
		"passes.takeover.received_only", "massage.parties", "massage.slots",
		"massage.bookings", "massage.provider.read", "privileges.events",
		"credits.usage", "credits.history", "credits.admin.usage",
		"credits.admin.history", "credits.admin.default", "credits.admin.policy",
		"broadcasts.preview", "broadcasts.pending", "broadcasts.attach",
		"broadcasts.cancel", "broadcasts.audience", "broadcasts.profile",
		"passes.payments.queue", "passes.payments.history", "massage.practitioner.schedule",
		"massage.practitioner.preferences", "massage.practitioner.bookings", "preferences.get",
		"preferences.setLanguage", "profile.get", "profile.set",
		"profile.history", "models.effective", "models.own.get",
		"models.own.set", "models.others.get", "models.others.set",
		"models.global.get", "models.global.set", "models.grants.set",
		"massage.book", "massage.cancel", "massage.practitioner.instant",
		"massage.practitioner.configure", "broadcasts.review", "broadcasts.show",
		"memory.summary", "memory.index", "memory.search",
		"memory.read", "memory.history", "memory.revision",
		"memory.sources", "memory.write", "knowledge.read", "knowledge.scopes", "knowledge.proposals", "knowledge.review_queue",
		"knowledge.memos", "knowledge.memo_read", "knowledge.memo_set", "knowledge.memo_delete",
		"knowledge.suggest", "knowledge.curate", "knowledge.remove_fact", "knowledge.review_card",
		"memo.read", "history.read", "model.loop",
		"model.cache", "model.remote", "model.validation",
		"script.registry.resolve", "script.registry.list", "script.callback", "script.knowledge.refresh", "script.source.admit", "script.call.prepare", "script.call.admit", "script.call.execute", "script.call.complete":
		return operation
	default:
		return diagnosticUnknown
	}
}
