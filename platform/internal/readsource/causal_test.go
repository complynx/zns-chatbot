package readsource_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestCausalSourceBoundsAndClone(t *testing.T) {
	t.Parallel()
	generation := int64(0)
	malformed := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Causal: &readsource.CausalSource{Actor: "alice", Generation: &generation}},
		},
	}
	require.False(t, malformed.Valid())
	require.False(t, malformed.Clone().Valid(), "clone must preserve missing mandatory authority list")
	leaves := make([]readsource.Authority, readsource.MaxAuthorities-1)
	for i := range leaves {
		leaves[i] = readsource.Authority{
			Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: strconv.Itoa(i)},
		}
	}
	captured, err := readsource.Capture("alice", readsource.Derivation{Generation: &generation, Authorities: leaves})
	require.NoError(t, err)
	require.True(t, readsource.Valid(captured))
	_, err = readsource.Merge(
		captured,
		[]readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "overflow"}},
		},
	)
	require.ErrorIs(t, err, readsource.ErrLimit)
	clone := readsource.CloneAuthorities(captured)
	require.True(t, readsource.Equal(captured[0], clone[0]))
	clone[0].Causal.Authorities[0].Knowledge.Scope = "changed"
	require.False(t, readsource.Equal(captured[0], clone[0]))
	nested := readsource.Authority{
		Causal: &readsource.CausalSource{Actor: "bob", Generation: &generation, Authorities: captured},
	}
	require.False(t, readsource.Valid([]readsource.Authority{nested}))
}

func TestCausalRepeatedCaptureIsFlat(t *testing.T) {
	t.Parallel()
	generation := int64(4)
	first := readsource.Authority{
		Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "first"},
	}
	second := readsource.Authority{
		Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "second"},
	}
	refs, err := readsource.Capture(
		"alice",
		readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{first}},
	)
	require.NoError(t, err)
	refs, err = readsource.Capture(
		"alice",
		readsource.Derivation{Generation: &generation, Authorities: append(refs, second)},
	)
	require.NoError(t, err)
	again, err := readsource.Capture("alice", readsource.Derivation{Generation: &generation, Authorities: refs})
	require.NoError(t, err)
	merged, err := readsource.Merge(refs, again)
	require.NoError(t, err)
	require.Len(t, merged, 1)
	require.Len(t, merged[0].Causal.Authorities, 2)
	require.True(t, readsource.Equal(refs[0], again[0]))
}
