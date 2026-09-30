package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptModelsEffective = "models.effective"
const scriptModelsGrant = "models.grants.set"

// Each capability has separate bindings so discovery never advertises a setter
// to an actor who cannot use it. The domain service checks the current grant again.
func (b *Bot) scriptModelEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return (agenthost.ModelScriptCatalog{
		ReadPermissions: func(ctx context.Context, owner string) (map[string]bool, error) {
			var permissions map[string]bool
			err := b.API.Call(ctx, owner, http.MethodGet, "/v1/model-settings/permissions", nil, &permissions)
			return permissions, err
		},
		Binding: agenthost.ScriptToolEntry{Prepare: b.prepareScriptModel, Execute: b.executeScriptModel, ResultLimit: maxOrdinaryScriptResult},
	}).Entries(ctx, owner)
}

func modelToolEndpoint(name, actor, target string) (string, error) {
	switch name {
	case scriptModelsEffective:
		return "/v1/model-settings/effective", nil
	case "models.own.get", "models.own.set":
		return "/v1/model-settings", nil
	case "models.global.get", "models.global.set":
		return "/v1/model-settings/default", nil
	case "models.others.get", "models.others.set":
		if target != "" && target != actor && target != modelsettings.GlobalScope && len(target) <= 200 {
			return "/v1/model-settings/users/" + url.PathEscape(target), nil
		}
	case scriptModelsGrant:
		if target != "" && target != modelsettings.GlobalScope && len(target) <= 200 {
			return "/v1/model-settings/grants", nil
		}
	}
	return "", errors.New("invalid model tool target")
}

func (b *Bot) prepareScriptModel(
	ctx context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	args, err := decodeModelTool(call)
	if err != nil {
		return record, err
	}
	endpoint, err := modelToolEndpoint(call.Name, owner, args.Owner)
	if err != nil {
		return record, err
	}
	record.Model = &agenthost.ScriptModelRequest{Endpoint: endpoint}
	if call.Name == scriptModelsGrant {
		record.Model.Grant = &modelsettings.Grant{
			Owner:      args.Owner,
			Capability: args.Capability,
			Enabled:    *args.Enabled,
		}
	} else if strings.HasSuffix(call.Name, ".set") {
		var current modelsettings.State
		if err = b.API.Call(ctx, owner, http.MethodGet, endpoint, nil, &current); err != nil {
			return record, err
		}
		record.Model.Change = &modelsettings.Change{
			Model: *args.Model, Effort: *args.Effort,
			Version: current.Version,
		}
		record.Model.Scope = owner
		switch call.Name {
		case "models.global.set":
			record.Model.Scope = modelsettings.GlobalScope
		case "models.others.set":
			record.Model.Scope = args.Owner
		}
	}
	return record, nil
}

func (b *Bot) executeScriptModel(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	if record.Model == nil {
		return nil, errors.New("model tool binding missing")
	}
	request := record.Model
	if request.Grant != nil {
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		err := b.Host.GrantDerivedModelSettings(ctx, owner, *request.Grant, *record.Source)
		return struct {
			OK bool `json:"ok"`
		}{OK: err == nil}, err
	}
	if call.Name == scriptModelsEffective {
		var selection modelsettings.Selection
		err := b.API.Call(ctx, owner, http.MethodGet, request.Endpoint, nil, &selection)
		return selection, err
	}
	var state modelsettings.State
	var err error
	if request.Change != nil {
		if record.Source == nil || !record.Source.Valid() {
			return nil, errors.New("missing admitted source")
		}
		if request.Scope == "" {
			return nil, errors.New("model scope binding missing")
		}
		state, err = b.Host.SetDerivedModelSettings(ctx, owner, request.Scope, *request.Change, *record.Source)
	} else {
		err = b.API.Call(ctx, owner, http.MethodGet, request.Endpoint, nil, &state)
	}
	if core.IsDatabaseFailure(err) {
		return nil, err
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok &&
		problem.Status == http.StatusConflict && problem.Code == "stale_model_settings" {
		return nil, appclient.ErrReadStale
	}
	return state, err
}
