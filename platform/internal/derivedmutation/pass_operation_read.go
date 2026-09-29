package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// PassOperationInput contains domain parameters from an admitted operation.
// The execution ledger and its IDs, timestamps and tool names belong to the host.
type PassOperationInput struct {
	Command    *passbooking.Command          `json:"command,omitempty"`
	Assignment *passbooking.AdminAssignment  `json:"assignment,omitempty"`
	Batch      *passbooking.RuntimeBatch     `json:"batch,omitempty"`
	Menu       *PassOperationMenu            `json:"menu,omitempty"`
	Export     bool                          `json:"export,omitempty"`
	Source     *readsource.Derivation        `json:"source,omitempty"`
	Witness    *passbooking.OperationWitness `json:"witness,omitempty"`
	Retired    bool                          `json:"retired,omitempty"`
}

type PassOperationMenu struct {
	Event      string `json:"event"`
	Historical bool   `json:"historical"`
}

// PassOperationRead is atomic receipt evidence. ReadAuthorities are host-only.
type PassOperationRead struct {
	Summary         PassOperationSummary   `json:"summary"`
	ReadAuthorities []readsource.Authority `json:"read_authorities"`
}

func (r PassOperationInput) Validate() error {
	count := 0
	for _, present := range []bool{r.Command != nil, r.Assignment != nil, r.Batch != nil, r.Menu != nil, r.Export} {
		if present {
			count++
		}
	}
	if count != 1 || r.Source != nil && !r.Source.Valid() {
		return unavailablePassOperation()
	}
	return nil
}

// ReadPassOperation reads canonical receipts and live rights. It does not read
// or decode the host execution ledger, and it never reconstructs script output.
func (s Service) ReadPassOperation(
	ctx context.Context,
	actor string,
	input PassOperationInput,
) (PassOperationRead, error) {
	if err := input.Validate(); err != nil {
		return PassOperationRead{}, err
	}
	if input.Retired && input.Menu == nil && !input.Export {
		return s.readRetiredPassOperation(ctx, actor, input)
	}
	return s.readPassOperation(ctx, actor, input)
}

func (r PassOperationInput) event() string {
	switch {
	case r.Command != nil:
		return r.Command.Event
	case r.Assignment != nil:
		return r.Assignment.Event
	case r.Batch != nil:
		return r.Batch.Event
	case r.Menu != nil:
		return r.Menu.Event
	default:
		return ""
	}
}
