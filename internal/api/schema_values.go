package api

const (
	notificationStateRetryable    = "retryable"
	notificationStateInflight     = "send_inflight"
	notificationStateDelivered    = "delivered"
	notificationStateUnknown      = "delivery_unknown"
	notificationStateFailed       = "delivery_failed"
	notificationStateBlocked      = "delivery_blocked"
	notificationStateDismissed    = "dismissed"
	schemaOrder                   = "order"
	settingsSchemaParallelTasks   = "max_parallel_tasks"
	settingsSchemaQuestionTimeout = "question_timeout_seconds"
)
