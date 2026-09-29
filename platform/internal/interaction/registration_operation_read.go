package interaction

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type RegistrationOperationDomain interface {
	ReadPassOperation(
		context.Context,
		string,
		derivedmutation.PassOperationInput,
	) (derivedmutation.PassOperationRead, error)
}

// RegistrationOperations composes host admissions with live domain receipts.
// It owns selection and presentation, while the ledger remains the only store.
type RegistrationOperations struct {
	Ledger RegistrationOperationReader
	Domain RegistrationOperationDomain
}

func (c RegistrationOperations) Read(
	ctx context.Context,
	owner string,
	query derivedmutation.PassOperationQuery,
) (RegistrationOperationRead, error) {
	if err := query.Validate(); err != nil {
		return RegistrationOperationRead{}, err
	}
	admissions, err := c.Ledger.ReadRegistrationOperations(ctx, owner, query.ID)
	if err != nil {
		return RegistrationOperationRead{}, err
	}
	result := RegistrationOperationRead{
		Summaries:       []RegistrationOperationSummary{},
		ReadAuthorities: []readsource.Authority{},
	}
	for _, admitted := range admissions {
		value, readErr := c.Domain.ReadPassOperation(ctx, owner, registrationOperationInput(admitted))
		if readErr != nil {
			if registrationOperationDenied(readErr) {
				continue
			}
			return RegistrationOperationRead{}, readErr
		}
		refs, mergeErr := readsource.Merge(result.ReadAuthorities, value.ReadAuthorities)
		if mergeErr != nil {
			return RegistrationOperationRead{}, mergeErr
		}
		result.ReadAuthorities = refs
		result.Summaries = append(
			result.Summaries,
			registrationOperationSummary(admitted, value.Summary, query.ID == ""),
		)
	}
	if query.ID != "" && len(result.Summaries) == 0 {
		return RegistrationOperationRead{}, &core.ProblemError{
			Status: http.StatusNotFound,
			Code:   "pass_operation_unavailable",
		}
	}
	return result, nil
}

func registrationOperationInput(a RegistrationOperation) derivedmutation.PassOperationInput {
	r := derivedmutation.PassOperationInput{
		Command:    a.Command,
		Assignment: a.Assignment,
		Batch:      a.Batch,
		Source:     a.Source,
		Export:     a.Tool == "passes.export",
		Witness:    a.Witness,
		Retired:    a.Retired,
	}
	if a.Menu != nil {
		r.Menu = &derivedmutation.PassOperationMenu{Event: a.Menu.Event, Historical: a.Menu.Historical}
	}
	return r
}

func registrationOperationSummary(
	a RegistrationOperation,
	value derivedmutation.PassOperationSummary,
	preview bool,
) RegistrationOperationSummary {
	r := RegistrationOperationSummary{
		ID:           a.ID,
		Tool:         a.Tool,
		AdmittedAt:   a.AdmittedAt,
		Status:       value.Status,
		Continuation: value.Continuation,
		Items:        value.Items,
		Committed:    value.Committed,
		Pending:      value.Pending,
	}
	if value.Context != nil {
		v := value.Context
		r.Context = &RegistrationOperationContext{
			Event:          v.Event,
			Target:         v.Target,
			Recipients:     v.Recipients,
			RecipientCount: v.RecipientCount,
		}
	}
	if preview {
		r.Items = nil
		const recipientPreview = 3
		if r.Context != nil && len(r.Context.Recipients) > recipientPreview {
			r.Context.Recipients = r.Context.Recipients[:recipientPreview]
		}
	}
	return r
}

func registrationOperationDenied(err error) bool {
	var problem *core.ProblemError
	return errors.As(err, &problem) && (problem.Status == http.StatusForbidden || problem.Status == http.StatusNotFound)
}
