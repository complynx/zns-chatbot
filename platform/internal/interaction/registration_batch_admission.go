package interaction

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type RegistrationBatchInputStore interface {
	LoadBatchInput(context.Context, string, int64) (passbooking.RuntimeBatch, bool, error)
	SaveBatchInput(context.Context, string, int64, passbooking.RuntimeBatch) error
}

type RegistrationBatchEvents interface {
	PassEvents(context.Context, string) ([]passbooking.Event, error)
}

var ErrRegistrationBatchEvent = errors.New("no registration event available")
var ErrRegistrationBatchInputMissing = errors.New("saved registration batch missing")

// RegistrationBatchAdmission owns durable manual admission and event selection.
// Execution, per-item authorization, provenance and receipts remain domain owned.
type RegistrationBatchAdmission struct {
	Store  RegistrationBatchInputStore
	Events RegistrationBatchEvents
	Now    func() time.Time
}

// Manual resolves saved identity before interpreting a retry. Decode adapts the
// trusted transport command only when no durable input exists. The winner is
// reloaded after insertion so concurrent admissions cannot execute different
// event/recipient/options bindings under the same owner and update key.
func (c RegistrationBatchAdmission) Manual(ctx context.Context, owner string, updateID int64,
	decode func() (passbooking.RuntimeBatch, error),
) (passbooking.RuntimeBatch, error) {
	saved, found, err := c.Store.LoadBatchInput(ctx, owner, updateID)
	if err != nil {
		return passbooking.RuntimeBatch{}, err
	}
	if found {
		return saved, nil
	}
	command, err := decode()
	if err != nil {
		return passbooking.RuntimeBatch{}, err
	}
	command.Key = "telegram-pass-batch-" + strconv.FormatInt(updateID, 10)
	if command.Event == "" {
		events, readErr := c.Events.PassEvents(ctx, owner)
		if readErr != nil {
			return passbooking.RuntimeBatch{}, readErr
		}
		if len(events) == 0 {
			return passbooking.RuntimeBatch{}, ErrRegistrationBatchEvent
		}
		now := time.Now
		if c.Now != nil {
			now = c.Now
		}
		command.Event = passbooking.ClosestEvent(events, now())
	}
	if err = c.Store.SaveBatchInput(ctx, owner, updateID, command); err != nil {
		return passbooking.RuntimeBatch{}, err
	}
	saved, found, err = c.Store.LoadBatchInput(ctx, owner, updateID)
	if err != nil {
		return passbooking.RuntimeBatch{}, err
	}
	if !found {
		return passbooking.RuntimeBatch{}, ErrRegistrationBatchInputMissing
	}
	return saved, nil
}

// BindGroundedRegistrationBatch admits current-request event, recipient and
// assignment evidence. It leaves the key empty: the existing host reservation
// binds its immutable operation key/source after this construction, and resume
// uses that saved command without invoking this function again.
func BindGroundedRegistrationBatch(evidence string, input agent.Input, action, event string,
	recipients []int64, options *agent.RegistrationAssignment,
) (passbooking.RuntimeBatch, error) {
	if input.Registration == nil || !RegistrationEventKnown(input.Registration, event) || event == "" ||
		len(recipients) == 0 || len(recipients) > passbooking.MaxAdminBatchRecipients {
		return passbooking.RuntimeBatch{}, errors.New("pass batch lacks evidence")
	}
	for _, id := range recipients {
		if !RegistrationAdminTargetGrounded(evidence, input,
			agent.RegistrationProposal{Event: event, Target: strconv.FormatInt(id, 10)}) {
			return passbooking.RuntimeBatch{}, errors.New("pass recipient lacks evidence")
		}
	}
	command := passbooking.RuntimeBatch{Event: event, Action: action, Recipients: append([]int64{}, recipients...)}
	if action != agent.RegistrationAdminAssign {
		if options != nil {
			return passbooking.RuntimeBatch{}, errors.New("invalid batch options")
		}
		return command, nil
	}
	if options == nil {
		options = &agent.RegistrationAssignment{}
	}
	proposal := agent.RegistrationProposal{
		Name: agent.RegistrationAdminAssign, Event: event, Target: "batch", Assignment: options,
	}
	if err := agent.Validate(agent.Plan{View: agent.RegistrationView, RegistrationAction: &proposal}); err != nil {
		return passbooking.RuntimeBatch{}, err
	}
	if options.LegalName != nil && !strings.Contains(evidence, *options.LegalName) {
		return passbooking.RuntimeBatch{}, errors.New("assignment name lacks current evidence")
	}
	command.Options = passbooking.AdminAssignment{TotalPrice: options.TotalPrice, Kind: options.Kind,
		Comment: options.Comment, SkipBalance: options.SkipBalance, AppendTier: options.AppendTier}
	if options.Create {
		command.Options.Create = &passbooking.AdminCreate{FromProfile: options.FromProfile,
			Role: passallocation.Role(options.Role), LegalName: options.LegalName}
	}
	return command, nil
}
