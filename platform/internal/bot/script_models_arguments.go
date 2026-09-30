package bot

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptModelArguments struct {
	Owner      string  `json:"owner,omitempty"`
	Model      *string `json:"model,omitempty"`
	Effort     *string `json:"effort,omitempty"`
	Capability string  `json:"capability,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

// Decode each operation's exact shape, including required booleans and strings.
func decodeModelTool(call scriptclient.ToolCall) (scriptModelArguments, error) {
	var args scriptModelArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return args, err
	}
	invalid := errors.New("invalid model tool arguments")
	keys := []string{}
	if strings.HasPrefix(call.Name, "models.others.") {
		keys = append(keys, ownerField)
	}
	if strings.HasSuffix(call.Name, ".set") {
		keys = append(keys, "model", "effort")
	}
	if call.Name == scriptModelsGrant {
		keys = []string{ownerField, "capability", "enabled"}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &fields); err != nil {
		return args, err
	}
	for key := range fields {
		if !slices.Contains(keys, key) {
			return args, invalid
		}
	}
	if call.Name == scriptModelsGrant {
		if args.Enabled == nil ||
			(args.Capability != modelsettings.Own && args.Capability != modelsettings.Others && args.Capability != modelsettings.Global) {
			return args, invalid
		}
		return args, nil
	}
	if strings.HasSuffix(call.Name, ".set") {
		if args.Model == nil || args.Effort == nil ||
			!modelsettings.Valid(modelsettings.Selection{Model: *args.Model, Effort: *args.Effort}) {
			return args, invalid
		}
	}
	return args, nil
}
