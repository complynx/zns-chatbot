package bot

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptPassArguments struct {
	Event            string                        `json:"event"`
	Target           string                        `json:"target"`
	InviteTelegramID int64                         `json:"invite_telegram_id"`
	PaymentAdmin     string                        `json:"payment_admin"`
	Assignment       *agent.RegistrationAssignment `json:"assignment"`
}

func (b *Bot) preparePassTool(
	ctx context.Context,
	owner string,
	updateID int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	switch call.Name {
	case scriptPassShow:
		request, err := b.preparePassShow(ctx, owner, updateID, call, input)
		record.Pass = request
		return record, err
	case scriptPassResume:
		var args struct {
			ID string `json:"operation_id"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return record, err
		}
		request, originalSource, err := b.loadPassOperation(ctx, owner, args.ID)
		record.Pass, record.Source = request, originalSource
		return record, err
	case scriptPassExport:
		if err := decodeScriptArguments(call.Arguments, &struct{}{}); err != nil {
			return record, err
		}
		source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
		if !ok || source.owner != owner {
			return record, errors.New("pass delivery source unavailable")
		}
		record.Pass = &agenthost.ScriptPassRequest{
			ID:           rand.Text(),
			Name:         call.Name,
			ExportUpdate: updateID,
			Chat:         source.in.chat,
		}
		return record, nil
	case scriptPassBatchAssign, scriptPassBatchCancel, scriptRegistrationBatchUncouple:
		request, err := preparePassBatch(call, input)
		record.Pass = request
		return record, err
	case scriptPassOperations:
		return record, decodeScriptArguments(call.Arguments, &derivedmutation.PassOperationQuery{})
	case scriptPassRead,
		scriptPassAdminRead,
		scriptPassAdminTarget,
		scriptPassReviewRead,
		scriptPassTakeoverRead:
		return record, nil // The read executor decodes its view-specific arguments before fetching.
	case scriptPassTiers:
		var reference agenthost.ScriptDomainEventArguments
		if err := decodeScriptArguments(call.Arguments, &reference); err != nil {
			return record, err
		}
		record.PassRead = &reference
		return record, nil
	}
	var args scriptPassArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	proposal := agent.RegistrationProposal{
		Name:             agenthost.PassToolActions()[call.Name],
		Event:            args.Event,
		Target:           args.Target,
		InviteTelegramID: args.InviteTelegramID,
		PaymentAdmin:     args.PaymentAdmin,
		Assignment:       args.Assignment,
	}
	plan := agent.Plan{View: agent.RegistrationView, RegistrationAction: &proposal}
	if err := agent.Validate(plan); err != nil {
		return record, err
	}
	request := &agenthost.ScriptPassRequest{ID: rand.Text(), Name: call.Name}
	var err error
	if proposal.Name == agent.RegistrationAdminAssign {
		request.Assignment, _, err = interaction.BindAdminAssignment(
			agenthost.CurrentRequestEvidence(input),
			plan,
			input,
		)
	} else {
		request.Command, _, err = interaction.BindRegistrationPlan(agenthost.CurrentRequestEvidence(input), plan, input)
	}
	if err != nil {
		return record, err
	}
	record.Pass = request
	return record, nil
}

func (b *Bot) authorizePassRequest(ctx context.Context, owner string, request *agenthost.ScriptPassRequest) error {
	if request.Name == scriptPassExport {
		capability, err := b.API.PassToolCapabilities(ctx, owner)
		if err != nil {
			return err
		}
		if !capability.Export {
			return errors.New("pass export unavailable")
		}
		return nil
	}
	if request.Menu != nil {
		booking, err := b.API.PassBooking(ctx, owner, request.Menu.Event)
		if err == nil && request.Menu.Historical && booking.Version == 0 {
			return &core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden}
		}
		return err
	}
	var event string
	switch {
	case request.Command != nil:
		event = request.Command.Event
	case request.Assignment != nil:
		event = request.Assignment.Event
	case request.Batch != nil:
		event = request.Batch.Event
	default:
		return errors.New("pass operation unavailable")
	}
	capability, err := b.API.PassCapabilities(ctx, owner, event)
	if err != nil {
		return err
	}
	if !slices.Contains(capability.Actions, agenthost.PassToolActions()[request.Name]) {
		return errors.New("pass operation unavailable")
	}
	return nil
}

func (b *Bot) preparePassShow(
	ctx context.Context,
	owner string,
	updateID int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (*agenthost.ScriptPassRequest, error) {
	var args struct {
		Event string `json:"event"`
		View  string `json:"view"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	switch args.View {
	case "", passMenuHome, passMenuInvitations, registrationAdminsView, registrationPayment:
	default:
		return nil, errors.New("invalid pass view")
	}
	proposal := agent.RegistrationProposal{Name: agent.RegistrationShow, Event: args.Event, View: args.View}
	plan := agent.Plan{View: agent.RegistrationView, RegistrationAction: &proposal}
	if err := agent.Validate(plan); err != nil {
		return nil, err
	}
	if input.Registration != nil && !interaction.RegistrationEventKnown(input.Registration, args.Event) &&
		!interaction.RegistrationHistoricalEventKnown(input.Registration, args.Event) {
		read := proposal
		read.Name = agent.RegistrationRead
		if err := b.performRegistrationRead(ctx, owner, updateID, read, &input); err != nil {
			return nil, err
		}
	}
	_, menu, err := interaction.BindRegistrationPlan(agenthost.CurrentRequestEvidence(input), plan, input)
	if err != nil {
		return nil, err
	}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return nil, errors.New("pass source unavailable")
	}
	return &agenthost.ScriptPassRequest{
		ID:           rand.Text(),
		Name:         call.Name,
		Menu:         menu,
		ExportUpdate: updateID,
		Chat:         source.in.chat,
	}, nil
}
