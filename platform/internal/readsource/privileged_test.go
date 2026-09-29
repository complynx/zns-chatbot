package readsource_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPrivilegedLeavesRemainDistinctThroughCapture(t *testing.T) {
	t.Parallel()
	refs := []readsource.Authority{
		{Registration: passbooking.ReadAuthority{Kind: passbooking.ReadPaymentRole, Event: "a"}},
		{Food: legacyfood.ReadAuthority{Event: "a", Scope: "review"}},
		{Food: legacyfood.ReadAuthority{Event: "b", Scope: "review"}},
		{Practitioner: massage.ReadAuthority{Event: "a", Owner: "actor"}},
		{Practitioner: massage.ReadAuthority{Event: "a", Owner: "other"}},
	}
	merged, err := readsource.Merge(refs, refs)
	require.NoError(t, err)
	require.Len(t, merged, len(refs))
	generation := int64(0)
	captured, err := readsource.Capture("actor", readsource.Derivation{Generation: &generation, Authorities: merged})
	require.NoError(t, err)
	encoded, err := json.Marshal(captured)
	require.NoError(t, err)
	var saved []readsource.Authority
	require.NoError(t, json.Unmarshal(encoded, &saved))
	require.True(t, readsource.Valid(saved))
	require.True(t, readsource.Equal(captured[0], saved[0]))
	saved[0].Causal.Authorities[0].Food = legacyfood.ReadAuthority{Event: "other", Scope: "export"}
	require.False(t, readsource.Equal(captured[0], saved[0]))
	for _, bad := range []readsource.Authority{
		{Food: legacyfood.ReadAuthority{Event: "a", Scope: "any"}},
		{Practitioner: massage.ReadAuthority{Event: "a"}},
		{Food: refs[1].Food, Practitioner: refs[3].Practitioner},
		{Causal: captured[0].Causal, Food: refs[1].Food},
	} {
		require.False(t, readsource.Valid([]readsource.Authority{bad}))
	}
}
