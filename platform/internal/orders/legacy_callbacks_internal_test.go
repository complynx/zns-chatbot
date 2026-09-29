package orders

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyCallbackGrammar(t *testing.T) {
	t.Parallel()
	id := "0123456789abcdef01234567"
	for _, data := range []string{
		"orders|start", "orders|close", "orders|xlsx", "orders|del|" + id,
		"orders|pay|" + id, "orders|paid|" + id, "orders|cash|" + id + "|202",
		"orders|adm_acc|" + id, "orders|adm_rej|" + id + "|token", "orders|pcancel|" + id + "|",
	} {
		_, err := parseLegacyCallback(data)
		require.NoError(t, err, data)
	}
	for _, data := range []string{
		"food|start", "orders|", "orders|unknown", "orders|start|77", "orders|pay|no-id",
		"orders|cash|" + id + "|-1", "orders|del|" + id + "|token", "orders|pcancel|" + id + "|x|y",
		"orders|adm_acc|" + id + "|" + strings.Repeat("a", 64),
	} {
		_, err := parseLegacyCallback(data)
		require.Error(t, err, data)
	}
}

func TestLegacyPaymentGuardPreservesSourceTokenPresence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, source, callback, attempt, state string
		allowed                                bool
	}{
		{"token present", `{"payment_attempt_token":"a"}`, "a", "a", "proof", true},
		{"missing callback token", `{"payment_attempt_token":"a"}`, "", "a", "proof", false},
		{"empty source field differs from absent", `{"payment_attempt_token":""}`, "", "", "cash", false},
		{"null source field differs from absent", `{"payment_attempt_token":null}`, "", "", "cash", false},
		{"legacy proof", `{"proof_file":"telegram-file"}`, "", "legacy-proof:telegram-file", "proof", true},
		{"legacy cash", `{"proof_file":"cash"}`, "", "", "cash", true},
		{"new payment attempt", `{"proof_file":"cash"}`, "", "new-attempt", "cash", false},
		{"completed payment", `{"proof_file":"cash"}`, "", "", "paid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var source map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(test.source), &source))
			err := legacyPaymentGuard(test.callback, Order{State: test.state, Attempt: test.attempt}, source)
			assert.Equal(t, test.allowed, err == nil)
		})
	}
}
