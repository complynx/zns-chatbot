package agenthost

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRetiredMemoryProjectionRejectsUnrelatedFailures(t *testing.T) {
	t.Parallel()
	stale := errors.New("stale")
	denied := errors.New("permission denied")
	for _, failure := range []error{nil, denied, errors.Join(stale, denied),
		errors.Join(stale, context.Canceled), fmt.Errorf("wrapped: %w", errors.Join(stale, denied))} {
		assert.False(t, onlyScriptStale(failure, stale))
	}
	assert.True(t, onlyScriptStale(errors.Join(fmt.Errorf("wrapped: %w", stale)), stale))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	host := ScriptHost{Store: ScriptStore{StaleError: stale}}
	projected, err := host.projectRetiredMemory(ctx, "alice", 1, 0, &agent.Input{}, stale)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, projected, "cancelled context must not read or project the ledger")
}

func TestRetiredMemoryProjectionRequiresMemoryRetirement(t *testing.T) {
	t.Parallel()
	interrupted := agent.ScriptRun{Error: scriptInterrupted}
	assert.True(t, memoryRetirementProjection(ScriptRecord{MemoryRedacted: true, Run: interrupted}))
	assert.True(t, memoryRetirementProjection(ScriptRecord{MemoryRedacted: true, PassRedacted: true, Run: interrupted}))
	assert.False(t, memoryRetirementProjection(ScriptRecord{PassRedacted: true, Run: interrupted}))
	assert.False(
		t,
		memoryRetirementProjection(ScriptRecord{MemoryRedacted: true, HistoryRedacted: true, Run: interrupted}),
	)
	assert.False(t, memoryRetirementProjection(ScriptRecord{MemoryRedacted: true}))
	assert.False(t, memoryRetirementProjection(ScriptRecord{Run: interrupted}))
}

func TestMemoryProjectionWitnessPreservesAuthorityAndStoredEpochs(t *testing.T) {
	t.Parallel()
	state := knowledge.MemoryDeletionState{PrivateGeneration: 7, SharedGeneration: 9}
	original := ScriptRecord{
		PassRedacted:   true,
		MemoryRedacted: true,
		MemoryState:    state,
		ReadAuthorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.PrivateMemory, Generation: 1}},
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.SharedMemory, Generation: 2}},
			{Registration: passbooking.ReadAuthority{Kind: passbooking.ReadPaymentRole, Event: "exact-event"}},
			{
				Causal: &readsource.CausalSource{
					Actor: "bob",
					Authorities: []readsource.Authority{
						{
							Knowledge: knowledgeauthority.ReadAuthority{
								Kind:       knowledgeauthority.PrivateMemory,
								Generation: 3,
							},
						},
					},
				},
			},
		},
		Calls: []ScriptToolRecord{
			{
				MemoryReadState: &knowledge.MemoryDeletionState{PrivateGeneration: 1},
				Source: &readsource.Derivation{
					Authorities: []readsource.Authority{
						{
							Knowledge: knowledgeauthority.ReadAuthority{
								Kind:       knowledgeauthority.PrivateMemory,
								Generation: 1,
							},
						},
					},
				},
			},
		},
	}
	witness := memoryProjectionWitness("alice", original)
	assert.True(t, witness.PassRedacted, "caller clears retirement flag only for the access check")
	assert.EqualValues(t, 7, witness.ReadAuthorities[0].Knowledge.Generation)
	assert.EqualValues(t, 9, witness.ReadAuthorities[1].Knowledge.Generation)
	assert.Equal(t, original.ReadAuthorities[2], witness.ReadAuthorities[2])
	assert.EqualValues(t, 3, witness.ReadAuthorities[3].Causal.Authorities[0].Knowledge.Generation)
	assert.Equal(t, state, *witness.Calls[0].MemoryReadState)
	assert.EqualValues(t, 7, witness.Calls[0].Source.Authorities[0].Knowledge.Generation)
	assert.EqualValues(t, 1, original.ReadAuthorities[0].Knowledge.Generation)
	assert.EqualValues(t, 2, original.ReadAuthorities[1].Knowledge.Generation)
	assert.EqualValues(t, 1, original.Calls[0].MemoryReadState.PrivateGeneration)
	assert.EqualValues(t, 1, original.Calls[0].Source.Authorities[0].Knowledge.Generation)
}
