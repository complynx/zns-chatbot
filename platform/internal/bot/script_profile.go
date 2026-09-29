package bot

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const maxScriptProfileResult = 8 * 1024

func (b *Bot) scriptProfileEntries() []scriptToolEntry {
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	return []scriptToolEntry{
		{
			descriptor: scriptclient.Tool{
				Name:        "preferences.get",
				Description: "Read your current language preferences.",
				InputSchema: empty,
			},
			prepare:     prepareScriptProfileRead,
			execute:     b.executeScriptPreferencesRead,
			resultLimit: maxOrdinaryScriptResult,
		},
		{
			descriptor: scriptclient.Tool{
				Name:        "profile.get",
				Description: "Read your own pass profile, including saved personal details. Return only details needed for the current request.",
				InputSchema: empty,
			},
			prepare: prepareScriptProfileRead,
			execute: b.executeScriptProfileRead,
			// Two 300-character fields may each expand sixfold when JSON encoded.
			resultLimit: maxScriptProfileResult,
		},
	}
}

func prepareScriptProfileRead(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	return record, decodeScriptArguments(call.Arguments, &struct{}{})
}

func (b *Bot) executeScriptPreferencesRead(
	ctx context.Context,
	owner string,
	_ scriptclient.ToolCall,
	_ scriptToolRecord,
	_ *agent.Input,
) (any, error) {
	return b.API.Preferences(ctx, owner)
}

func (b *Bot) executeScriptProfileRead(
	ctx context.Context,
	owner string,
	_ scriptclient.ToolCall,
	_ scriptToolRecord,
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
