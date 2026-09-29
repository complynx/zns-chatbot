package appclient

import (
	"errors"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const knowledgeEventQuery = "event"
const knowledgeTopicQuery = "topic"
const knowledgeKeyQuery = "key"
const memoryCursorQuery = "cursor"
const massagePartyQuery = "party"
const massageLength = "length"
const passAfterQuery = "after"

var ErrReadStale = errors.New("stale order read")
var ErrReadLimit = errors.New("read result limit")
var ErrProvisioningDenied = errors.New("identity provisioning denied")

func ReadError(err error) error {
	if p, ok := errors.AsType[*core.ProblemError](err); ok {
		switch p.Code {
		case "read_stale", "history_stale":
			return ErrReadStale
		case "read_result_limit":
			return ErrReadLimit
		}
	}
	return err
}
func memoryQueryValues(q knowledge.MemoryQuery) url.Values {
	return url.Values{
		"namespace":         {q.Namespace},
		knowledgeEventQuery: {q.Event},
		"topic":             {q.Topic},
		"q":                 {q.Text},
		"mode":              {q.Mode},
		"cursor":            {q.Cursor},
	}
}
func registrationPaymentPath(event, owner string) string {
	return "/v1/passes/events/" + url.PathEscape(event) + "/participants/" + url.PathEscape(owner) + "/payment"
}
