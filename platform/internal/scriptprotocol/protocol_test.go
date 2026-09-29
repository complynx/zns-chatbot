package scriptprotocol_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

func TestCatalogRejectsPrototypeAndNamespaceCollisions(t *testing.T) {
	t.Parallel()
	for _, names := range [][]string{{"__proto__.read"}, {"orders.constructor"}, {"orders.$help"}, {"orders", "orders.get"}, {"orders.get", "orders"}, {"orders.get", "orders.get"}} {
		tools := make([]scriptprotocol.Tool, 0, len(names))
		for _, name := range names {
			tools = append(tools, scriptprotocol.Tool{Name: name})
		}
		require.Error(t, scriptprotocol.ValidateTools(tools))
	}
	require.NoError(
		t,
		scriptprotocol.ValidateTools([]scriptprotocol.Tool{{Name: "orders.get"}, {Name: "orders.change"}}),
	)
}

func TestBoundedCatalogBeyondSixteen(t *testing.T) {
	t.Parallel()
	tools := make([]scriptprotocol.Tool, 0, scriptprotocol.MaxTools)
	for index := range scriptprotocol.MaxTools {
		tools = append(
			tools,
			scriptprotocol.Tool{
				Name: fmt.Sprintf("tool%03d.%s.%s", index, strings.Repeat("a", 59), strings.Repeat("b", 59)),
			},
		)
	}
	require.NoError(t, scriptprotocol.ValidateTools(tools))
	data, err := json.Marshal(tools)
	require.NoError(t, err)
	require.Less(t, len(data), 32<<10)
	require.Error(t, scriptprotocol.ValidateTools(append(tools, scriptprotocol.Tool{Name: "overflow"})))
	for index := range tools {
		tools[index].InputSchema = json.RawMessage(`{"description":"` + strings.Repeat("s", 1024) + `"}`)
	}
	require.Error(
		t,
		scriptprotocol.ValidateTools(tools),
		"full metadata remains bounded independently of descriptor count",
	)
}
