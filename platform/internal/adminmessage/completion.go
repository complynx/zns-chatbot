package adminmessage

// MaxRetryAfterSeconds bounds an upstream cooldown to one day. A larger or
// negative value must stop automatic retries instead of retrying prematurely.
const MaxRetryAfterSeconds int64 = 24 * 60 * 60

// Completion records the fenced attempt and its next permitted retry time.
type Completion struct {
	ID         int64  `json:"id"`
	Attempt    int64  `json:"attempt"`
	MessageID  int64  `json:"message_id"`
	Failure    string `json:"failure"`
	Retry      bool   `json:"retry"`
	RetryAfter int64  `json:"retry_after,omitempty"`
}
