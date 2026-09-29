package bot

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func registrationAdminTargetGrounded(input agent.Input, p agent.RegistrationProposal) bool {
	id, err := strconv.ParseInt(p.Target, 10, 64)
	if err != nil || input.Registration == nil {
		return false
	}
	if groundedRegistrationContact(input, id) || input.Registration.AdminTargetTelegramID == id {
		return true
	}
	for _, read := range input.Registration.Reads {
		if read.Error != "" || read.Request.Event != p.Event || read.Request.View != passMenuQueue {
			continue
		}
		for _, booking := range read.Queue {
			if booking.TelegramID == id {
				return true
			}
		}
	}
	return false
}

func bindAdminAssignment(plan agent.Plan, input agent.Input) (*passbooking.AdminAssignment, *passMenuState, error) {
	p := plan.RegistrationAction
	if p == nil || p.Assignment == nil || input.Registration == nil {
		return nil, nil, errors.New("assignment context required")
	}
	target := adminAssignmentEvidence(input.Registration, p.Event, p.Target)
	if target == nil || !target.CanAssign {
		return nil, nil, errors.New("assignment target lacks evidence")
	}
	options := p.Assignment
	command := &passbooking.AdminAssignment{Event: p.Event, Version: target.ActorVersion,
		Target: target.Booking.Owner, TargetVersion: target.Booking.Version, TotalPrice: options.TotalPrice,
		Kind: options.Kind, Comment: options.Comment, SkipBalance: options.SkipBalance, AppendTier: options.AppendTier}
	if options.Create {
		if options.FromProfile && !target.CanCreateFromProfile {
			return nil, nil, errors.New("profile cannot supply assignment defaults")
		}
		if options.LegalName != nil && !strings.Contains(currentRequestEvidence(input), *options.LegalName) {
			return nil, nil, errors.New("assignment name lacks current evidence")
		}
		command.Create = &passbooking.AdminCreate{
			ProfileVersion: target.ProfileVersion,
			FromProfile:    options.FromProfile,
			Role:           passallocation.Role(options.Role),
			LegalName:      options.LegalName,
		}
	}
	state := &passMenuState{
		Event:                 p.Event,
		View:                  agent.RegistrationAdminTarget,
		AdminTargetTelegramID: target.Booking.TelegramID,
	}
	return command, state, nil
}

func adminAssignmentEvidence(context *agent.RegistrationContext, event, owner string) *passbooking.AdminTarget {
	for _, read := range slices.Backward(context.Reads) {
		if read.Error == "" && read.Request.Event == event && read.Request.View == agent.RegistrationAdminTarget &&
			read.AdminTarget != nil && read.AdminTarget.Booking.Owner == owner {
			return read.AdminTarget
		}
	}
	return nil
}

func bindAdminTargetView(context *agent.RegistrationContext, p agent.RegistrationProposal, state *passMenuState) error {
	if p.View != agent.RegistrationAdminTarget || p.Target == "" {
		return nil
	}
	target := adminAssignmentEvidence(context, p.Event, p.Target)
	if target == nil {
		return errors.New("assignment target lacks evidence")
	}
	state.AdminTargetTelegramID = target.Booking.TelegramID
	return nil
}

func (b *Bot) executeAdminAssignment(
	ctx context.Context,
	in incoming,
	id int64,
	command passbooking.AdminAssignment,
) (string, error) {
	command.Key = "tg-admin-assignment-" + strconv.FormatInt(id, 10)
	_, err := b.API.AssignPass(ctx, in.owner, command)
	return b.registrationExecutionNotice(ctx, in, id, agent.RegistrationAdminAssign, command.Event, err)
}
