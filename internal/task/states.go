package task

import "time"

const (
	stateAwaitingService         = "awaiting_service"
	deliveryDelivered            = "delivered"
	deliveryPending              = "pending"
	deliverySending              = "send_inflight"
	stateCompleted               = "completed"
	stateClosed                  = "closed"
	lockLossStopTimeout          = 3 * time.Second
	kindUnsupported              = "unsupported"
	kindApproval                 = "approval"
	kindMessage                  = "message"
	kindStructured               = "structured"
	stateFailed                  = "failed"
	stateCancelled               = "cancelled"
	stateAuthUnknown             = "auth_unknown"
	stateDeliveryUnknown         = "delivery_unknown"
	stateDeliveryFailed          = "delivery_failed"
	stateAwaitingAuth            = "awaiting_authorization"
	deliveryRetryable            = "retryable"
	maxChoices                   = 100
	maxBootstrapAttempts         = 3
	loomAuthorizationRetryWindow = 5 * time.Minute
	sessionRenewSkew             = 15 * time.Second
	controlSessionSkew           = 5 * time.Second
	defaultAttemptTimeout        = 20 * time.Second
	defaultLeaseMargin           = 5 * time.Second
)
