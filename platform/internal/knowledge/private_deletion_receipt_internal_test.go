package knowledge

import (
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPrivateDeletionSourcePreservesOriginalAndUnrelatedEvidence(t *testing.T) {
	t.Parallel()
	generation := int64(7)
	own := readsource.Authority{Knowledge: knowledgeauthority.ReadAuthority{
		Kind: knowledgeauthority.DerivedMemory, Namespace: MemoryPrivate, Owner: "alice",
		Topic: memoryPrivateTopic("diet"), Key: "diet", SourceKind: MemoryMemoKind, Version: 2,
	}}
	permission := readsource.Authority{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}}
	epoch := readsource.Authority{
		Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.PrivateMemory, Generation: 3},
	}
	foreign := readsource.Authority{Causal: &readsource.CausalSource{Actor: "bob", Generation: &generation,
		Authorities: []readsource.Authority{epoch}}}
	source := readsource.Derivation{
		Generation:  &generation,
		Authorities: []readsource.Authority{own, permission, epoch, foreign},
	}
	original := source.Clone()
	witness := PrivateDeletionWitness{SourceKind: MemoryMemoKind, BeforeHistory: 7, AfterHistory: 9,
		BeforeMemory: MemoryDeletionState{PrivateGeneration: 3}, AfterMemory: MemoryDeletionState{PrivateGeneration: 4}}
	changed, ok := privateDeletionSource(
		"alice",
		Command{Name: MemoDelete, FactKey: "diet", Version: 2},
		source,
		witness,
	)
	require.True(t, ok)
	require.Equal(t, original, source)
	require.Equal(t, int64(9), *changed.Generation)
	require.Len(t, changed.Authorities, 3)
	require.Equal(t, permission, changed.Authorities[0])
	require.Equal(t, int64(4), changed.Authorities[1].Knowledge.Generation)
	require.Equal(t, foreign, changed.Authorities[2])
	witness.AfterMemory.SharedGeneration = 1
	_, ok = privateDeletionSource("alice", Command{Name: MemoDelete, FactKey: "diet", Version: 2}, source, witness)
	require.False(t, ok)
}

func TestPrivateDeletionSourceRequiresExactKindAndRevision(t *testing.T) {
	t.Parallel()
	generation := int64(7)
	memo := readsource.Authority{Knowledge: knowledgeauthority.ReadAuthority{
		Kind: knowledgeauthority.DerivedMemory, Namespace: MemoryPrivate, Owner: "alice",
		Topic: memoryPrivateTopic("diet"), Key: "diet", SourceKind: MemoryMemoKind, Version: 2,
	}}
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{memo}}
	witness := PrivateDeletionWitness{SourceKind: MemoryMemoKind, BeforeHistory: 7, AfterHistory: 8,
		BeforeMemory: MemoryDeletionState{}, AfterMemory: MemoryDeletionState{PrivateGeneration: 1}}
	document := Command{Name: DocumentDelete, Topic: memoryPrivateTopic("diet"), FactKey: "diet", Version: 2}
	_, valid := privateDeletionSource("alice", document, source, witness)
	require.False(t, valid, "memo witness cannot explain document deletion")
	changed, valid := privateDeletionSource(
		"alice",
		Command{Name: MemoDelete, FactKey: "diet", Version: 3},
		source,
		witness,
	)
	require.True(t, valid)
	require.Equal(t, source.Authorities, changed.Authorities, "different revision must remain for live validation")
	witness.SourceKind = MemoryDocumentKind
	changed, valid = privateDeletionSource("alice", document, source, witness)
	require.True(t, valid)
	require.Equal(t, source.Authorities, changed.Authorities, "same key in a different source kind must remain")
	documentLeaf := memo
	documentLeaf.Knowledge.SourceKind = MemoryDocumentKind
	source.Authorities = []readsource.Authority{documentLeaf}
	changed, valid = privateDeletionSource("alice", document, source, witness)
	require.True(t, valid)
	require.Empty(t, changed.Authorities)
	require.Equal(t, []readsource.Authority{documentLeaf}, source.Authorities)
}
