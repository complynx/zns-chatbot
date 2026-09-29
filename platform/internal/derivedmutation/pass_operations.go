package derivedmutation

import (
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const operationReferenceLength = 26

type PassOperationQuery struct {
	ID string `json:"operation_id,omitempty"`
}

type PassOperationContext struct {
	Target         string  `json:"target,omitempty"`
	Event          string  `json:"event"`
	Recipients     []int64 `json:"recipients,omitempty"`
	RecipientCount int     `json:"recipient_count,omitempty"`
}

// PassOperationSummary is newly authorized receipt metadata, not a restored
// script result. No command bodies, comments, sources or receipt payloads escape.
type PassOperationSummary struct {
	Status       string                             `json:"status"`
	Continuation string                             `json:"continuation"`
	Context      *PassOperationContext              `json:"context,omitempty"`
	Items        []passbooking.OperationReceiptItem `json:"items,omitempty"`
	Committed    int                                `json:"committed_items"`
	Pending      int                                `json:"pending_items"`
}

func (q PassOperationQuery) Validate() error {
	if q.ID == "" {
		return nil
	}
	if len(q.ID) != operationReferenceLength {
		return unavailablePassOperation()
	}
	for _, c := range q.ID {
		if (c < 'A' || c > 'Z') && (c < '2' || c > '7') {
			return unavailablePassOperation()
		}
	}
	return nil
}

func unavailablePassOperation() error {
	return &core.ProblemError{Status: http.StatusNotFound, Code: "pass_operation_unavailable"}
}
