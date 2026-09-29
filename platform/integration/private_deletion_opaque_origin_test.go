package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPrivateDeletionRetainsOpaqueOriginPermission(t *testing.T) {
	t.Parallel()
	for _, name := range []string{knowledge.MemoDelete, knowledge.DocumentDelete} {
		for _, access := range []string{"allowed", "revoked"} {
			t.Run(name+"/"+access, func(t *testing.T) {
				t.Parallel()
				checkOpaqueDeletionOrigin(t, name, access == "revoked")
			})
		}
	}
}

func checkOpaqueDeletionOrigin(t *testing.T, name string, revoked bool) {
	t.Helper()
	f := knowledgeAuthorizationFixture(t)
	service := knowledge.Service{DB: f.db}
	source, command := prepareOpaqueDeletion(t, service, name)
	original := source.Clone()
	deleted, err := service.ExecuteDerived(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.NotNil(t, deleted.PrivateDeletion)
	if name == knowledge.MemoDelete {
		require.False(t, deleted.Memo.Active)
		require.Equal(t, int64(2), deleted.Memo.Version)
	} else {
		require.False(t, deleted.Document.Active)
		require.Equal(t, int64(2), deleted.Document.Version)
	}
	if revoked {
		_, err = f.db.Exec(
			t.Context(),
			"DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review'",
		)
		require.NoError(t, err)
	}
	receipt, found, err := service.CommandReceipt(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, receipt.PrivateDeletion)
	require.Equal(t, !revoked, receipt.PrivateDeletion.Current,
		"deleting the observed revision must retain its unrelated origin permission")
	require.Equal(t, original, source)
}

func prepareOpaqueDeletion(
	t *testing.T,
	service knowledge.Service,
	name string,
) (readsource.Derivation, knowledge.Command) {
	t.Helper()
	generation := int64(0)
	origin := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{
		{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
	}}
	seed := knowledge.Command{
		Name:    knowledge.MemoSet,
		Key:     "opaque-seed",
		FactKey: "diet",
		Text:    "OPAQUE-ORIGIN-PRIVATE-CANARY",
	}
	if name == knowledge.DocumentDelete {
		seed.Name, seed.Topic = knowledge.DocumentSet, "preferences"
	}
	_, err := service.ExecuteDerived(t.Context(), "bob", seed, origin)
	require.NoError(t, err)
	page, err := service.SearchMemory(t.Context(), "bob", knowledge.MemoryQuery{
		Namespace: knowledge.MemoryPrivate, Text: seed.Text, Mode: "literal",
	})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	entry, err := service.ReadMemory(t.Context(), "bob", page.Entries[0].Ref)
	require.NoError(t, err)
	require.Len(t, entry.ReadAuthorities, 1)
	require.Equal(t, knowledgeauthority.DerivedMemory, entry.ReadAuthorities[0].Knowledge.Kind)
	history, err := (conversation.Service{DB: service.DB}).Window(t.Context(), "bob", 1)
	require.NoError(t, err)
	source := readsource.Derivation{Generation: &history.Generation, Authorities: entry.ReadAuthorities}
	command := knowledge.Command{Name: name, Key: "opaque-delete", Topic: seed.Topic, FactKey: seed.FactKey, Version: 1}
	return source, command
}
