package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptProfileSet = "profile.set"
const scriptProfileHistory = "profile.history"
const scriptLanguageSet = "preferences.setLanguage"

// Script calls never resume interrupted mutations. Keep authority/effect metadata,
// but never copy identity values into the durable script receipt.

func (b *Bot) scriptProfileMutationEntries(canBook bool) []agenthost.ScriptToolEntry {
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptProfileHistory,
			Description: "Read your complete profile change metadata in pages, newest first. Pass next_before as before until absent. Identity values are never in history.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"before":{"type":"integer","minimum":0}},"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptLanguageSet,
			Description: "Set your language for current controls and subsequent requests only when explicitly requested. Choose en or ru; never infer a default.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"language":{"enum":["en","ru"]}},"required":["language"],"additionalProperties":false}`,
			),
		},
	}
	if canBook {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptProfileSet,
				Description: "Save your own legal_name, passport or dance role (leader/follower), only from an explicit current user request. Normalize the requested value; pending prompts are hints, never permission to consume unrelated text. Host binds actor/version/key. Returns metadata only. On stale or interrupted outcome read profile.get afresh before deciding next action.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"field":{"enum":["legal_name","passport","role"]},"value":{"type":"string","minLength":1,"maxLength":200}},"required":["field","value"],"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]agenthost.ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			agenthost.ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     b.prepareScriptProfileMutation,
				Execute:     b.executeScriptProfileMutation,
				ResultLimit: maxScriptProfileResult,
			},
		)
	}
	return entries
}

func (b *Bot) prepareScriptProfileMutation(
	ctx context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	switch call.Name {
	case scriptProfileHistory:
		var args struct {
			Before int64 `json:"before"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil || args.Before < 0 {
			return record, errors.New("invalid history cursor")
		}
	case scriptLanguageSet:
		var args struct {
			Language string `json:"language"`
		}
		if err := decodeScriptArguments(
			call.Arguments,
			&args,
		); err != nil || (args.Language != "en" && args.Language != "ru") ||
			strings.TrimSpace(input.Text) == "" {
			return record, errors.New("invalid language request")
		}
		record.Language = &agenthost.ScriptLanguageMutation{Language: args.Language}
	case scriptProfileSet:
		var args struct {
			Field string `json:"field"`
			Value string `json:"value"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return record, err
		}
		proposal := agent.ProfileProposal{Name: profileSet, Field: args.Field, Value: args.Value}
		if err := agent.Validate(agent.Plan{View: agent.ProfilesView, ProfileAction: &proposal}); err != nil {
			return record, err
		}
		if strings.TrimSpace(input.Text) == "" {
			return record, errors.New("profile request missing")
		}
		current, err := b.API.PassProfile(ctx, owner)
		if err != nil {
			return record, err
		}
		record.Profile = &agenthost.ScriptProfileMutation{
			Field:   args.Field,
			Value:   args.Value,
			Version: current.Version,
		}
	default:
		return record, errors.New("tool unavailable")
	}
	return record, nil
}

func (b *Bot) executeScriptProfileMutation(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	switch call.Name {
	case scriptProfileHistory:
		var args struct {
			Before int64 `json:"before"`
		}
		if err := decodeScriptArguments(call.Arguments, &args); err != nil {
			return nil, err
		}
		return b.API.PassProfileHistoryPage(ctx, owner, args.Before)
	case scriptLanguageSet:
		if record.Language == nil {
			return nil, errors.New("language binding missing")
		}
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		preference, err := b.Host.SetDerivedLanguage(
			ctx,
			owner,
			account.LanguageChange{Language: record.Language.Language, OperationKey: record.Language.Key},
			*record.Source,
		)
		if err == nil {
			input.Language = preference.Language
		}
		return preference, err
	case scriptProfileSet:
		if record.Profile == nil {
			return nil, errors.New("profile binding missing")
		}
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		change := record.Profile
		profile, err := b.Host.ExecuteDerivedPassProfile(
			ctx,
			owner,
			passes.Command{
				Name:    profileSet,
				Field:   change.Field,
				Value:   change.Value,
				Version: change.Version,
				Key:     change.Key,
				Origin:  originAgent,
			},
			*record.Source,
		)
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); !core.IsDatabaseFailure(err) && ok && problem.Status == http.StatusConflict &&
			problem.Code == "pass_profile_stale" {
			return nil, appclient.ErrReadStale
		}
		if err != nil {
			return nil, err
		}
		input.Profile = &agent.ProfileContext{
			Version:      profile.Version,
			Pending:      profile.Pending,
			Frozen:       profile.Frozen,
			HasLegalName: profile.LegalName != "",
			HasPassport:  profile.Passport != "",
			Role:         profile.Role,
		}
		return struct {
			Applied bool   `json:"applied"`
			Field   string `json:"field"`
			Version int64  `json:"version"`
		}{true, change.Field, profile.Version}, nil
	default:
		return nil, errors.New("tool unavailable")
	}
}
