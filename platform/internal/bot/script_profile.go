package bot

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const maxScriptProfileResult = 8 * 1024

func (b *Bot) scriptProfileEntries() []agenthost.ScriptToolEntry {
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	return []agenthost.ScriptToolEntry{
		{
			Descriptor: scriptclient.Tool{
				Name:        "preferences.get",
				Description: "Read your current language preferences.",
				InputSchema: empty,
			},
			Prepare:     prepareScriptProfileRead,
			Execute:     b.executeScriptPreferencesRead,
			ResultLimit: maxOrdinaryScriptResult,
		},
		{
			Descriptor: scriptclient.Tool{
				Name:        "profile.get",
				Description: "Read your own pass profile, including saved personal details. Return only details needed for the current request.",
				InputSchema: empty,
			},
			Prepare: prepareScriptProfileRead,
			Execute: b.executeScriptProfileRead,
			// Two 300-character fields may each expand sixfold when JSON encoded.
			ResultLimit: maxScriptProfileResult,
		},
	}
}

func prepareScriptProfileRead(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	return record, decodeScriptArguments(call.Arguments, &struct{}{})
}

func (b *Bot) executeScriptPreferencesRead(
	ctx context.Context,
	owner string,
	_ scriptclient.ToolCall,
	_ agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	return b.API.Preferences(ctx, owner)
}

func (b *Bot) executeScriptProfileRead(
	ctx context.Context,
	owner string,
	_ scriptclient.ToolCall,
	_ agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	profile, err := b.API.PassProfile(ctx, owner)
	if err == nil {
		input.Profile = &agent.ProfileContext{
			Version:      profile.Version,
			Pending:      profile.Pending,
			Frozen:       profile.Frozen,
			HasLegalName: profile.LegalName != "",
			HasPassport:  profile.Passport != "",
			Role:         profile.Role,
		}
	}
	return profile, err
}
