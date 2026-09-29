package bot

import (
	"context"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptRunScopeKey struct{}

// A running VM can call only its initial bindings. Live authorization may
// remove names; newly granted bindings become available in the next run.
func scriptRunScope(tools []scriptclient.Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

func restrictScriptRegistry(ctx context.Context, entries []scriptToolEntry) []scriptToolEntry {
	names, scoped := ctx.Value(scriptRunScopeKey{}).(map[string]bool)
	if !scoped {
		return entries
	}
	return slices.DeleteFunc(entries, func(entry scriptToolEntry) bool {
		return !names[entry.descriptor.Name]
	})
}
