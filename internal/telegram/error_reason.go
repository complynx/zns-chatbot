package telegram

import "net/http"

// ReasonClass returns only a fixed label, never Telegram's response description.
func (e *Error) ReasonClass() string {
	if e.StatusCode == http.StatusTooManyRequests {
		return "rate_limited"
	}
	if e.StatusCode >= http.StatusInternalServerError {
		return "server_error"
	}
	if e.reasonClass != "" {
		return e.reasonClass
	}
	if e.StatusCode == 0 {
		return "transport_or_validation"
	}
	return "other_rejection"
}

func rejectionReasonClass(description string) string {
	switch description {
	case "Bad Request: chat not found":
		return "chat_not_found"
	case "Forbidden: bot was blocked by the user":
		return "bot_blocked"
	case "Forbidden: user is deactivated":
		return "user_deactivated"
	default:
		return ""
	}
}
