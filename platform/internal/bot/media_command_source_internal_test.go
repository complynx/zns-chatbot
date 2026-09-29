package bot

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestMediaCommandSourceRequiresImmutableAgentEvidence(t *testing.T) {
	t.Parallel()
	manual, err := mediaCommandSource(originManual, nil)
	require.NoError(t, err)
	require.False(t, manual.Valid())
	_, err = mediaCommandSource(originAgent, nil)
	require.Error(t, err)
	_, err = mediaCommandSource(originAgent, &readsource.Derivation{})
	require.Error(t, err)
	generation := int64(0)
	source := &readsource.Derivation{
		PrivateHistory: true,
		Generation:     &generation,
		Authorities:    []readsource.Authority{},
	}
	cloned, err := mediaCommandSource(originAgent, source)
	require.NoError(t, err)
	generation = 9
	require.Zero(t, *cloned.Generation)
	require.True(t, cloned.PrivateHistory)
	require.NotNil(t, cloned.Authorities)
}
