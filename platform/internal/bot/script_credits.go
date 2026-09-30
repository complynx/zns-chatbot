package bot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptCreditHistory = "credits.history"
const scriptCreditAdminUsage = "credits.admin.usage"
const scriptCreditAdminHistory = "credits.admin.history"
const scriptCreditDefault = "credits.admin.default"
const scriptCreditPolicy = "credits.admin.policy"

type creditToolArguments struct {
	Payer   string `json:"payer,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Amount  string `json:"amount,omitempty"`
	Version int64  `json:"version,omitempty"`
}

func (b *Bot) scriptCreditEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return (agenthost.CreditScriptCatalog{
		ReadPermissions: func(ctx context.Context, owner string) (map[string]bool, error) {
			var permissions map[string]bool
			err := b.API.Call(ctx, owner, http.MethodGet, "/v1/credits/permissions", nil, &permissions)
			return permissions, err
		},
		Binding: agenthost.ScriptToolEntry{Prepare: prepareCreditTool, Execute: b.executeCreditTool, ResultLimit: maxScriptReadBytes},
	}).Entries(ctx, owner)
}

func prepareCreditTool(
	_ context.Context,
	_ string,
	updateID int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var args creditToolArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	if len(args.Payer) > 256 || len(args.Cursor) > 2048 {
		return record, errors.New("invalid credit arguments")
	}
	if call.Name == scriptCreditPolicy {
		change, valid := creditPolicyChange(args.Amount, args.Version, updateID)
		if !valid || args.Payer == "" || args.Version < 1 {
			return record, errors.New("invalid credit policy")
		}
		digest := sha256.Sum256(call.Arguments)
		change.OperationKey = fmt.Sprintf("script-credit-%d-%x", updateID, digest[:16])
		record.CreditPolicy = &agenthost.CreditToolCommand{Payer: args.Payer, Change: change}
	}
	return record, nil
}

func (b *Bot) executeCreditTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	var args creditToolArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	endpoint := "/v1/credits"
	switch call.Name {
	case scriptCreditPolicy:
		return b.executeCreditPolicy(ctx, owner, record)
	case scriptCreditDefault:
		var policy credits.Policy
		err := b.API.Call(ctx, owner, http.MethodGet, endpoint+"/default", nil, &policy)
		return creditToolPolicy(policy), err
	case scriptCreditAdminUsage, scriptCreditAdminHistory:
		if args.Payer == "" {
			return nil, errors.New("payer required")
		}
		endpoint += "/users/" + url.PathEscape(args.Payer)
	}
	if call.Name == scriptCreditHistory || call.Name == scriptCreditAdminHistory {
		var page core.ReadPage[credits.AttemptReport]
		err := b.API.Call(
			ctx,
			owner,
			http.MethodGet,
			endpoint+"/history?cursor="+url.QueryEscape(args.Cursor),
			nil,
			&page,
		)
		return creditToolHistory(page), err
	}
	var report credits.UsageReport
	err := b.API.Call(ctx, owner, http.MethodGet, endpoint+"/usage", nil, &report)
	return creditToolUsage(report), err
}

func (b *Bot) executeCreditPolicy(ctx context.Context, owner string, record agenthost.ScriptToolRecord) (any, error) {
	if record.CreditPolicy == nil {
		return nil, errors.New("credit policy unavailable")
	}
	if record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("missing admitted source")
	}
	command := record.CreditPolicy
	policy, err := b.Host.SetDerivedCreditPolicy(ctx, owner, command.Payer, command.Change, *record.Source)
	return creditToolPolicy(policy), err
}
