package bot

import (
	"context"
	"crypto/rand"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
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
	command, err := interaction.BindGroundedRegistrationBatch(agenthost.CurrentRequestEvidence(input), input,
		agenthost.PassToolActions()[call.Name], args.Event, args.Recipients, args.Assignment)
	if err != nil {
		return nil, err
	}
	return &agenthost.ScriptPassRequest{
		ID:    rand.Text(),
		Name:  call.Name,
		Batch: &command,
	}, nil
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
