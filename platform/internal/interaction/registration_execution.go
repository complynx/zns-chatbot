package interaction

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type RegistrationCommandClient interface {
	ExecutePassBooking(context.Context, string, passbooking.Command) (passbooking.Booking, error)
	AssignPass(context.Context, string, passbooking.AdminAssignment) (passbooking.AdminAssignmentResult, error)
	RunPassBatch(context.Context, string, passbooking.RuntimeBatch) ([]passbooking.RuntimeBatchItem, error)
}

type DerivedRegistrationClient interface {
	ExecuteDerivedPassBooking(
		context.Context,
		string,
		passbooking.Command,
		readsource.Derivation,
	) (passbooking.Booking, error)
	AssignDerivedPass(
		context.Context,
		string,
		passbooking.AdminAssignment,
		readsource.Derivation,
	) (passbooking.AdminAssignmentResult, error)
	RunDerivedPassBatch(
		context.Context,
		string,
		passbooking.RuntimeBatch,
		readsource.Derivation,
	) ([]passbooking.RuntimeBatchItem, error)
}

type RegistrationReceiptClient interface {
	PassBookingReceipt(
		context.Context,
		string,
		passbooking.Command,
		readsource.Derivation,
	) (derivedmutation.Receipt[passbooking.Booking], error)
	PassAssignmentReceipt(
		context.Context,
		string,
		passbooking.AdminAssignment,
		readsource.Derivation,
	) (derivedmutation.Receipt[passbooking.AdminAssignmentResult], error)
}

// RegistrationExecutionRecord preserves the existing registration_action payload.
// The injected writer owns the admitted update ID and the existing interaction row.
type RegistrationExecutionRecord struct {
	Action    string `json:"action"`
	Event     string `json:"event"`
	Committed string `json:"committed"`
}

type RegistrationExecutionWriter func(context.Context, string, RegistrationExecutionRecord) error

// ErrRegistrationExecutionRecord distinguishes a failed durable write from an
// expected domain refusal, including when both causes are present.
var ErrRegistrationExecutionRecord = errors.New("record registration execution")

// RegistrationExecutor routes already-bound commands and records their durable
// outcome before presentation. Domains retain authorization, receipts and locks.
// A nil Writer preserves consumers that do not use registration_action rows.
type RegistrationExecutor struct {
	Manual   RegistrationCommandClient
	Derived  DerivedRegistrationClient
	Receipts RegistrationReceiptClient
	Writer   RegistrationExecutionWriter
}

func (c RegistrationExecutor) Command(ctx context.Context, owner string,
	command passbooking.Command, source *readsource.Derivation,
) (passbooking.Booking, error) {
	var result passbooking.Booking
	var err error
	if source == nil {
		result, err = c.Manual.ExecutePassBooking(ctx, owner, command)
	} else {
		result, err = c.Derived.ExecuteDerivedPassBooking(ctx, owner, command, source.Clone())
	}
	return result, c.record(ctx, owner, command.Name, command.Event, err)
}

func (c RegistrationExecutor) Assignment(ctx context.Context, owner string,
	command passbooking.AdminAssignment, source *readsource.Derivation,
) (passbooking.AdminAssignmentResult, error) {
	var result passbooking.AdminAssignmentResult
	var err error
	if source == nil {
		result, err = c.Manual.AssignPass(ctx, owner, command)
	} else {
		result, err = c.Derived.AssignDerivedPass(ctx, owner, command, source.Clone())
	}
	return result, c.record(ctx, owner, agent.RegistrationAdminAssign, command.Event, err)
}

// Batch preserves canonical per-item outcomes and never writes registration_action.
// A nil error does not imply that every batch item committed.
func (c RegistrationExecutor) Batch(ctx context.Context, owner string,
	command passbooking.RuntimeBatch, source *readsource.Derivation,
) ([]passbooking.RuntimeBatchItem, error) {
	var result []passbooking.RuntimeBatchItem
	var err error
	if source == nil {
		result, err = c.Manual.RunPassBatch(ctx, owner, command)
	} else {
		result, err = c.Derived.RunDerivedPassBatch(ctx, owner, command, source.Clone())
	}
	return result, err
}

// RecoverCommand only probes the exact saved command. A missing or denied
// receipt does not write success metadata and never falls back to execution.
func (c RegistrationExecutor) RecoverCommand(ctx context.Context, owner string,
	command passbooking.Command, source readsource.Derivation,
) (derivedmutation.Receipt[passbooking.Booking], error) {
	receipt, err := c.Receipts.PassBookingReceipt(ctx, owner, command, source.Clone())
	if err != nil || !receipt.Found {
		return receipt, err
	}
	return receipt, c.record(ctx, owner, command.Name, command.Event, nil)
}

func (c RegistrationExecutor) RecoverAssignment(ctx context.Context, owner string,
	command passbooking.AdminAssignment, source readsource.Derivation,
) (derivedmutation.Receipt[passbooking.AdminAssignmentResult], error) {
	receipt, err := c.Receipts.PassAssignmentReceipt(ctx, owner, command, source.Clone())
	if err != nil || !receipt.Found {
		return receipt, err
	}
	return receipt, c.record(ctx, owner, agent.RegistrationAdminAssign, command.Event, nil)
}

func (c RegistrationExecutor) record(ctx context.Context, owner, action, event string, domainErr error) error {
	if c.Writer == nil {
		return domainErr
	}
	if domainErr != nil {
		problem, ok := errors.AsType[*core.ProblemError](domainErr)
		if !ok || problem.Status >= http.StatusInternalServerError ||
			errors.Is(domainErr, context.Canceled) || errors.Is(domainErr, context.DeadlineExceeded) {
			return domainErr
		}
	}
	err := c.Writer(ctx, owner, RegistrationExecutionRecord{
		Action: action, Event: event, Committed: strconv.FormatBool(domainErr == nil),
	})
	if err != nil {
		return errors.Join(domainErr, ErrRegistrationExecutionRecord, err)
	}
	return domainErr
}
