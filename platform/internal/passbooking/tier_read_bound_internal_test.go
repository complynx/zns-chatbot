package passbooking

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func TestTierReadExactByteBound(t *testing.T) {
	t.Parallel()
	value := TierStatus{}
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	value.Event = strings.Repeat("x", core.ReadResourceBytes-len(encoded)-1)
	require.NoError(t, checkTierResult(value))
	value.Event += "x"
	require.Error(t, checkTierResult(value))
	value.Event = strings.Repeat("<", core.ReadResourceBytes/6)
	require.Error(t, checkTierResult(value))
	minimum, err := json.Marshal(passallocation.Tier{Promo: true, BlockedByDate: true})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(minimum), minimumTierBytes)
}

// Byte identity preserves the existing wire format; JSONEq would ignore formatting changes.
func TestTierLegacyWireRepresentation(t *testing.T) {
	t.Parallel()
	tier, err := json.Marshal(passallocation.Tier{Amount: 17, Price: 29, Promo: true})
	require.NoError(t, err)
	require.Zero(
		t,
		bytes.Compare(
			[]byte(`{"Amount":17,"Price":29,"Start":"0001-01-01T00:00:00Z","Promo":true,"BlockedByDate":false}`),
			tier,
		),
	)
	counts, err := json.Marshal(passallocation.Counts{Leader: 7, Follower: 9})
	require.NoError(t, err)
	require.Zero(t, bytes.Compare([]byte(`{"Leader":7,"Follower":9}`), counts))
}
