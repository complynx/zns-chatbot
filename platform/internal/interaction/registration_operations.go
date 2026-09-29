package interaction

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// RegistrationOperation is the immutable admission recorded by the execution
// ledger. Results and transcript text are not part of this projection.
type RegistrationOperation struct {
	ID         string
	Tool       string
	AdmittedAt time.Time
	Command    *passbooking.Command
	Assignment *passbooking.AdminAssignment
	Batch      *passbooking.RuntimeBatch
	Menu       *RegistrationOperationMenu
	Source     *readsource.Derivation
	Witness    *passbooking.OperationWitness
	Retired    bool
}

type RegistrationOperationMenu struct {
	Event      string
	Historical bool
}

// RegistrationOperationReader reads at most 20 distinct admissions for the
// supplied owner, newest first. An empty ID selects that recent window; an exact
// ID selects only that owner's admission. Domain authorization remains the
// coordinator's responsibility. The reader must not reconstruct cleared data.
type RegistrationOperationReader interface {
	ReadRegistrationOperations(context.Context, string, string) ([]RegistrationOperation, error)
}

// RegistrationOperationRead couples newly authorized visible metadata with its
// host-only provenance. The host must project Summaries before model delivery
// and retain ReadAuthorities in its existing result-evidence record.
type RegistrationOperationRead struct {
	Summaries       []RegistrationOperationSummary `json:"summaries"`
	ReadAuthorities []readsource.Authority         `json:"read_authorities"`
}

type RegistrationOperationContext struct {
	Event          string  `json:"event"`
	Target         string  `json:"target,omitempty"`
	Recipients     []int64 `json:"recipients,omitempty"`
	RecipientCount int     `json:"recipient_count,omitempty"`
}

// RegistrationOperationSummary contains receipt facts and only currently
// authorized selection context. It contains no original result or source body.
type RegistrationOperationSummary struct {
	ID           string                             `json:"operation_id"`
	Tool         string                             `json:"tool"`
	AdmittedAt   time.Time                          `json:"admitted_at"`
	Status       string                             `json:"status"`
	Continuation string                             `json:"continuation"`
	Context      *RegistrationOperationContext      `json:"context,omitempty"`
	Items        []passbooking.OperationReceiptItem `json:"items,omitempty"`
	Committed    int                                `json:"committed_items"`
	Pending      int                                `json:"pending_items"`
}
