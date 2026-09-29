package bot

import (
	"context"
	"crypto/rand"
	"errors"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func preparePassBatch(call scriptclient.ToolCall, input agent.Input) (*agenthost.ScriptPassRequest, error) {
	var args struct {
		Event      string                        `json:"event"`
		Recipients []int64                       `json:"recipients"`
		Assignment *agent.RegistrationAssignment `json:"assignment"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	if input.Registration == nil || !interaction.RegistrationEventKnown(input.Registration, args.Event) ||
		args.Event == "" ||
		len(args.Recipients) == 0 ||
		len(args.Recipients) > passbooking.MaxAdminBatchRecipients {
		return nil, errors.New("pass batch lacks evidence")
	}
	for _, id := range args.Recipients {
		if !interaction.RegistrationAdminTargetGrounded(agenthost.CurrentRequestEvidence(input), input,
			agent.RegistrationProposal{Event: args.Event, Target: strconv.FormatInt(id, 10)},
		) {
			return nil, errors.New("pass recipient lacks evidence")
		}
	}
	request := &agenthost.ScriptPassRequest{
		ID:   rand.Text(),
		Name: call.Name,
		Batch: &passbooking.RuntimeBatch{
			Event:      args.Event,
			Action:     agenthost.PassToolActions()[call.Name],
			Recipients: args.Recipients,
		},
	}
	if call.Name != scriptPassBatchAssign {
		if args.Assignment != nil {
			return nil, errors.New("invalid batch options")
		}
		return request, nil
	}
	options := args.Assignment
	if options == nil {
		options = &agent.RegistrationAssignment{}
	}
	proposal := agent.RegistrationProposal{
		Name:       agent.RegistrationAdminAssign,
		Event:      args.Event,
		Target:     "batch",
		Assignment: options,
	}
	if err := agent.Validate(agent.Plan{View: agent.RegistrationView, RegistrationAction: &proposal}); err != nil {
		return nil, err
	}
	if options.LegalName != nil && !strings.Contains(agenthost.CurrentRequestEvidence(input), *options.LegalName) {
		return nil, errors.New("assignment name lacks current evidence")
	}
	request.Batch.Options = passbooking.AdminAssignment{
		TotalPrice:  options.TotalPrice,
		Kind:        options.Kind,
		Comment:     options.Comment,
		SkipBalance: options.SkipBalance,
		AppendTier:  options.AppendTier,
	}
	if options.Create {
		request.Batch.Options.Create = &passbooking.AdminCreate{
			FromProfile: options.FromProfile,
			Role:        passallocation.Role(options.Role),
			LegalName:   options.LegalName,
		}
	}
	return request, nil
}

type scriptPassBatchItem struct {
	TelegramID int64                        `json:"telegram_id"`
	Status     passbooking.AdminBatchStatus `json:"status"`
	Code       string                       `json:"code,omitempty"`
}

func (b *Bot) executePassBatch(
	ctx context.Context,
	owner string,
	request *agenthost.ScriptPassRequest,
	source readsource.Derivation,
) (any, error) {
	result, err := (interaction.RegistrationExecutor{Derived: b.Host}).Batch(ctx, owner, *request.Batch, &source)
	if err != nil {
		return nil, err
	}
	items := make([]scriptPassBatchItem, 0, len(result))
	for _, item := range result {
		items = append(
			items,
			scriptPassBatchItem{TelegramID: item.TelegramID, Status: item.Outcome.Status, Code: item.Outcome.Code},
		)
	}
	return items, nil
}
