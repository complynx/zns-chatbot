package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestLocalKnowledgeHTTPParity(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	signer := identity.Signer{Key: []byte("local-knowledge-test-key-32-bytes!")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.NewServices(s.DB, appservices.Options{}),
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{
		SandboxToken: signer.Token,
		LocalKnowledge: &appclient.LocalKnowledge{
			Service:    s,
			Authorizer: applicationauth.Authorizer{DB: s.DB, Verify: verify},
		},
	}
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "local-document",
		Topic:   "notes",
		FactKey: "private",
		Text:    "owner-private document",
	}
	created, err := local.ExecuteKnowledge(t.Context(), "alice", command)
	require.NoError(t, err)
	replayed, err := remote.ExecuteKnowledge(t.Context(), "alice", command)
	require.NoError(t, err)
	assertKnowledgeReplay(t, created, replayed)
	for _, c := range []appclient.Client{local, remote} {
		page, readErr := c.MemorySearch(t.Context(), "alice", knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate})
		require.NoError(t, readErr)
		require.Len(t, page.Entries, 1)
		entry, readErr := c.MemoryEntry(t.Context(), "alice", page.Entries[0].Ref, "")
		require.NoError(t, readErr)
		require.Equal(t, command.Text, entry.Text)
		_, readErr = c.MemoryEntry(t.Context(), "bob", page.Entries[0].Ref, "")
		require.Error(t, readErr)
		_, readErr = c.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{Name: "assess", Key: "user-assess", ProposalID: 1},
		)
		requireCode(t, readErr, "forbidden")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = c.Memos(ctx, "alice")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = c.ExecuteKnowledge(ctx, "alice", command)
		require.ErrorIs(t, readErr, context.Canceled)
	}
	localHost := appclient.Host{LocalKnowledge: local.LocalKnowledge, UserToken: local.UserToken}
	remoteHost := appclient.Host{Base: server.URL, HTTP: server.Client(), Signer: signer, UserToken: remote.UserToken}
	generation := int64(0)
	draftCommand := knowledge.Command{
		Name:    knowledge.Suggest,
		Key:     "local-proposal",
		Event:   "kb-current",
		Topic:   "travel",
		FactKey: "meeting",
		Text:    "Meet at noon",
	}
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	draft, err := localHost.ExecuteDerivedKnowledge(t.Context(), "alice", draftCommand, source)
	require.NoError(t, err)
	replayed, err = remoteHost.ExecuteDerivedKnowledge(t.Context(), "alice", draftCommand, source)
	require.NoError(t, err)
	assertKnowledgeReplay(t, draft, replayed)
	assessment := knowledge.Assessment{
		Key:        "local-assess",
		ProposalID: draft.Proposal.ID,
		Version:    draft.Proposal.Version,
		Worthwhile: true,
	}
	assessed, err := localHost.AssessMemoryProposal(t.Context(), "alice", assessment)
	require.NoError(t, err)
	replayed, err = remoteHost.AssessMemoryProposal(t.Context(), "alice", assessment)
	require.NoError(t, err)
	assertKnowledgeReplay(t, assessed, replayed)
	require.Equal(t, knowledge.AwaitingSubmission, assessed.Proposal.State)
	queue, err := local.KnowledgeProposals(
		t.Context(),
		"bob",
		knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true},
	)
	require.NoError(t, err)
	require.Empty(t, queue)
	consent := proposalSubmission(*assessed.Proposal)
	_, err = localHost.SubmitKnowledgeProposal(t.Context(), "bob", consent)
	require.Error(t, err)
	submitted, err := localHost.SubmitKnowledgeProposal(t.Context(), "alice", consent)
	require.NoError(t, err)
	replayed, err = remoteHost.SubmitKnowledgeProposal(t.Context(), "alice", consent)
	require.NoError(t, err)
	assertKnowledgeReplay(t, submitted, replayed)
	require.True(t, submitted.Proposal.Submitted)
	require.NoError(t, remoteHost.ArchiveOriginal(t.Context(), "alice", "tg-user-919", "user", "original source"))
	require.NoError(t, localHost.AttachMemorySources(t.Context(), "alice", command.Key, 919))
	require.NoError(t, remoteHost.AttachMemorySources(t.Context(), "alice", command.Key, 919))
	for _, c := range []appclient.Client{local, remote} {
		page, readErr := c.MemorySearch(t.Context(), "alice", knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate})
		require.NoError(t, readErr)
		sources, readErr := c.MemorySources(t.Context(), "alice", page.Entries[0].Ref)
		require.NoError(t, readErr)
		require.Len(t, sources.Events, 1)
		require.Equal(t, "original source", sources.Events[0].Text)
	}
	_, err = s.DB.Exec(
		t.Context(),
		`DELETE FROM core.knowledge_permissions WHERE actor='bob' AND scope='kb-current' AND permission='review'`,
	)
	require.NoError(t, err)
	for _, c := range []appclient.Client{local, remote} {
		_, err = c.KnowledgeProposals(
			t.Context(),
			"bob",
			knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true},
		)
		requireCode(t, err, "forbidden")
	}
}
