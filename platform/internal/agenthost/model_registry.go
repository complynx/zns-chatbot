package agenthost

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptModelsEffective = "models.effective"
const scriptModelsGrant = "models.grants.set"

// ModelScriptCatalog owns live permission-based visibility and schemas.
// ReadPermissions adapts the existing domain permission contract.
type ModelScriptCatalog struct {
	ReadPermissions func(context.Context, string) (map[string]bool, error)
	Binding         ScriptToolEntry
}

func (c ModelScriptCatalog) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	permissions, err := c.ReadPermissions(ctx, owner)
	if err != nil {
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
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     c.Binding.Prepare,
				Execute:     c.Binding.Execute,
				ResultLimit: c.Binding.ResultLimit,
			},
		)
	}
	return entries, nil
}
