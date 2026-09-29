package scriptworker_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestMemoryDocumentUnicodeInput(t *testing.T) {
	t.Parallel()
	input, err := json.Marshal(map[string]string{"text": strings.Repeat("🌿", 16000)})
	require.NoError(t, err)
	result, err := scriptworker.Evaluate(t.Context(), scriptworker.Request{
		Code: "return Array.from(input.text).length;", Input: input,
	})
	require.NoError(t, err)
	require.JSONEq(t, `16000`, string(result))
}
