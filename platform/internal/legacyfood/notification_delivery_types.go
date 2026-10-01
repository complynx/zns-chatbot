package legacyfood

import (
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// NotificationCompletion records the exact wire attempt before archive or view work.
type NotificationCompletion struct {
	ID      int64            `json:"id"`
	Attempt int64            `json:"attempt"`
	Outcome delivery.Outcome `json:"outcome"`
	Text    string           `json:"text,omitempty"`
}

// NotificationFollowup retries local follow-up work without another notification send.
type NotificationFollowup struct {
	ID      int64  `json:"id"`
	Attempt int64  `json:"attempt"`
	Done    bool   `json:"done"`
	Failure string `json:"failure,omitempty"`
}

// NotificationDeliveryStatus exposes bounded operational metadata without message content.
type NotificationDeliveryStatus struct {
	ID                      int64      `json:"id"`
	Attempt                 int64      `json:"attempt"`
	State                   string     `json:"state"`
	MessageID               int64      `json:"message_id,omitempty"`
	Reason                  string     `json:"reason,omitempty"`
	FailureCount            int64      `json:"failure_count"`
	AvailableAt             time.Time  `json:"available_at"`
	FollowupPending         bool       `json:"followup_pending"`
	FollowupFailure         string     `json:"followup_failure,omitempty"`
	FollowupAttempts        int64      `json:"followup_attempts"`
	LastUncertainAttempt    int64      `json:"last_uncertain_attempt,omitempty"`
	LastUncertainReason     string     `json:"last_uncertain_reason,omitempty"`
	LastUncertainRecordedAt *time.Time `json:"last_uncertain_recorded_at,omitempty"`
	UncertainResends        int64      `json:"uncertain_resends"`
}
