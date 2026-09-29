package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptModelsEffective = "models.effective"
const scriptModelsGrant = "models.grants.set"

type scriptModelRequest struct {
	Endpoint string                `json:"endpoint"`
	Change   *modelsettings.Change `json:"change,omitempty"`
	Grant    *modelsettings.Grant  `json:"grant,omitempty"`
}

// Each capability has separate bindings so discovery never advertises a setter
// to an actor who cannot use it. The domain service checks the current grant again.
func (b *Bot) scriptModelEntries(ctx context.Context, owner string) ([]scriptToolEntry, error) {
	var permissions map[string]bool
	if err := b.API.call(ctx, owner, http.MethodGet, "/v1/model-settings/permissions", nil, &permissions); err != nil {
		return nil, err
	}
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptModelsEffective,
			Description: "Read the effective model and reasoning for your dialogue.",
			InputSchema: empty,
		},
	}
	for _, scope := range []string{modelsettings.Own, modelsettings.Others, modelsettings.Global} {
		if !permissions[scope] {
			continue
		}
		namespace := "models." + scope
		properties, required := "", ""
		if scope == modelsettings.Others {
			properties = `"owner":{"type":"string","minLength":1,"maxLength":200},`
			required = `"owner",`
		}
		readSchema := empty
		if scope == modelsettings.Others {
			readSchema = json.RawMessage(
				`{"type":"object","properties":{"owner":{"type":"string","minLength":1,"maxLength":200}},"required":["owner"],"additionalProperties":false}`,
			)
		}
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        namespace + ".get",
				Description: "Read " + scope + " model settings and the supported model/reasoning catalog. Other-user targets must come from the user's request or authorized records.",
				InputSchema: readSchema,
			},
			scriptclient.Tool{
				Name:        namespace + ".set",
				Description: "Set " + scope + " model and reasoning for subsequent requests. Use empty model and effort to restore inheritance. Host binds current version and replay key; existing domain permissions apply.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{` + properties + `"model":{"type":"string","maxLength":100},"effort":{"type":"string","maxLength":30}},"required":[` + required + `"model","effort"],"additionalProperties":false}`,
				),
			},
		)
	}
	if permissions[modelsettings.GrantPermission] {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptModelsGrant,
				Description: "Grant or revoke only model-setting own/others/global permission for an explicitly selected user. Requires current superadmin authority; this cannot grant superadmin.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"owner":{"type":"string","minLength":1,"maxLength":200},"capability":{"enum":["own","others","global"]},"enabled":{"type":"boolean"}},"required":["owner","capability","enabled"],"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]scriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			scriptToolEntry{
				descriptor:  descriptor,
				prepare:     b.prepareScriptModel,
				execute:     b.executeScriptModel,
				resultLimit: maxOrdinaryScriptResult,
			},
		)
	}
	return entries, nil
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
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	args, err := decodeModelTool(call)
	if err != nil {
		return record, err
	}
	endpoint, err := modelToolEndpoint(call.Name, owner, args.Owner)
	if err != nil {
		return record, err
	}
	record.Model = &scriptModelRequest{Endpoint: endpoint}
	if call.Name == scriptModelsGrant {
		record.Model.Grant = &modelsettings.Grant{
			Owner:      args.Owner,
			Capability: args.Capability,
			Enabled:    *args.Enabled,
		}
	} else if strings.HasSuffix(call.Name, ".set") {
		var current modelsettings.State
		if err = b.API.call(ctx, owner, http.MethodGet, endpoint, nil, &current); err != nil {
			return record, err
		}
		record.Model.Change = &modelsettings.Change{
			Model: *args.Model, Effort: *args.Effort,
			Version: current.Version,
		}
	}
	return record, nil
}

func (b *Bot) executeScriptModel(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	_ *agent.Input,
) (any, error) {
	if record.Model == nil {
		return nil, errors.New("model tool binding missing")
	}
	request := record.Model
	if request.Grant != nil {
		var result struct {
			OK bool `json:"ok"`
		}
		err := b.API.call(ctx, owner, http.MethodPost, request.Endpoint, request.Grant, &result)
		return result, err
	}
	if call.Name == scriptModelsEffective {
		var selection modelsettings.Selection
		err := b.API.call(ctx, owner, http.MethodGet, request.Endpoint, nil, &selection)
		return selection, err
	}
	var state modelsettings.State
	method := http.MethodGet
	var input any
	if request.Change != nil {
		method, input = http.MethodPost, request.Change
	}
	err := b.API.call(ctx, owner, method, request.Endpoint, input, &state)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok &&
		problem.Status == http.StatusConflict && problem.Code == "stale_model_settings" {
		return nil, errScriptReadStale
	}
	return state, err
}
