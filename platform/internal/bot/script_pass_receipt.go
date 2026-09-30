package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// A retired receipt is a fresh read. It carries no executable command or old source.
func (b *Bot) preparePassResume(
	ctx context.Context, owner, id string, record agenthost.ScriptToolRecord,
) (agenthost.ScriptToolRecord, error) {
	if err := (derivedmutation.PassOperationQuery{ID: id}).Validate(); err != nil {
		return record, err
	}
	admissions, err := b.scriptHost().Store.ReadRegistrationOperations(ctx, owner, id)
	if err != nil {
		return record, err
	}
	if id != "" && len(admissions) == 1 && admissions[0].Retired {
		record.PassReceiptID = id
		return record, nil
	}
	record.Pass, record.Source, err = b.loadPassOperation(ctx, owner, id)
	return record, err
}

func (b *Bot) executePassReceipt(
	ctx context.Context, owner string, call scriptclient.ToolCall, record agenthost.ScriptToolRecord,
) (any, error) {
	if call.Name != scriptPassResume || record.Pass != nil || record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("pass operation unavailable")
	}
	return b.registrationOperations().ObserveReceipt(ctx, owner, record.PassReceiptID)
}
