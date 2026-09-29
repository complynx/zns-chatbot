package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestCausalMemoryReaderAndTerminalRevision(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	service := knowledge.Service{DB: f.db}
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
	_, err := service.ExecuteDerived(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.Curate,
			Key:     "derived-shared",
			Topic:   "travel",
			FactKey: "causal-shared",
			Text:    "causal secret canary",
		},
		source,
	)
	require.NoError(t, err)
	alice, err := service.SearchMemory(
		ctx,
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: "travel"},
	)
	require.NoError(t, err)
	encoded, err := json.Marshal(alice)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "causal secret canary")
	bob, err := service.SearchMemory(
		ctx,
		"bob",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Len(t, bob.Entries, 1)
	inherited := readsource.Derivation{Generation: &generation, Authorities: bob.Entries[0].ReadAuthorities}
	_, err = service.ExecuteDerived(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.DocumentSet,
			Key:     "copy",
			Topic:   "notes",
			FactKey: "copy",
			Text:    "causal copied canary",
		},
		inherited,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review'`)
	require.NoError(t, err)
	denied, err := service.SearchMemory(
		ctx,
		"bob",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Empty(t, denied.Entries)
	_, err = f.db.Exec(ctx, `INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`)
	require.NoError(t, err)
	restored, err := service.SearchMemory(
		ctx,
		"bob",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Empty(t, restored.Entries)
	copied, err := service.SearchMemory(
		ctx,
		"bob",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "notes"},
	)
	require.NoError(t, err)
	require.Empty(t, copied.Entries)
	_, err = service.ReadMemory(ctx, "bob", bob.Entries[0].Ref)
	require.Error(t, err)
	replay, replayErr := service.ExecuteDerived(ctx, "bob", knowledge.Command{
		Name:    knowledge.Curate,
		Key:     "derived-shared",
		Topic:   "travel",
		FactKey: "causal-shared",
		Text:    "causal secret canary",
	}, source)
	require.NoError(t, replayErr)
	require.True(t, replay.Redacted, "a terminal derived revision must also stay hidden in same-key operation replay")
	replayJSON, replayErr := json.Marshal(replay)
	require.NoError(t, replayErr)
	require.NotContains(t, string(replayJSON), "causal secret canary")
}

func TestCausalManualPublicationPreservesOrigin(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	service := knowledge.Service{DB: f.db}
	_, err := f.db.Exec(
		ctx,
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
	)
	require.NoError(t, err)
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.SharedMemory, Generation: 0}},
		},
	}
	draft, err := service.ExecuteDerived(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "derived-proposal",
			Topic:   "travel",
			FactKey: "published",
			Text:    "published causal canary",
		},
		source,
	)
	require.NoError(t, err)
	assessed, err := service.Assess(
		ctx,
		"bob",
		knowledge.Assessment{
			Key:        "assess",
			ProposalID: draft.Proposal.ID,
			Version:    draft.Proposal.Version,
			Worthwhile: true,
			Reason:     "useful",
		},
	)
	require.NoError(t, err)
	assessed = submitKnowledgeProposal(t, service, "bob", *assessed.Proposal)
	approved, err := service.Execute(
		ctx,
		"alice",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "manual-approve",
			ProposalID: draft.Proposal.ID,
			Version:    assessed.Proposal.Version,
			Decision:   "approve",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, approved.Fact)
	_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
	require.NoError(t, err)
	ordinary, err := service.SearchMemory(
		ctx,
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Len(t, ordinary.Entries, 1)
	require.Contains(t, ordinary.Entries[0].Text, "published causal canary")
	temporary, err := service.Execute(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.Curate,
			Key:     "temporary",
			Topic:   "notes",
			FactKey: "temporary",
			Text:    "temporary",
		},
	)
	require.NoError(t, err)
	_, err = service.Execute(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.RemoveFact,
			Key:     "delete-origin",
			Topic:   "notes",
			FactKey: "temporary",
			Version: temporary.Fact.Version,
		},
	)
	require.NoError(t, err)
	denied, err := service.SearchMemory(
		ctx,
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Empty(t, denied.Entries)
}

func TestCausalPrivateDraftAndOriginalReview(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	service := knowledge.Service{DB: f.db}
	original, err := service.Proposals(ctx, "bob", knowledge.ProposalQuery{ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, original, 1)
	require.Equal(t, "Private pending proposal", original[0].Text)
	approved, err := service.Execute(
		ctx,
		"bob",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "original-approve",
			ProposalID: original[0].ID,
			Version:    original[0].Version,
			Decision:   "approve",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Private pending proposal", approved.Fact.Text)
	_, err = f.db.Exec(
		ctx,
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
	)
	require.NoError(t, err)
	generation := int64(0)
	draft, err := service.ExecuteDerived(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "private-draft",
			Topic:   "travel",
			FactKey: "private-draft",
			Text:    "owner private draft canary",
		},
		readsource.Derivation{PrivateHistory: true, Generation: &generation, Authorities: []readsource.Authority{}},
	)
	require.NoError(t, err)
	assessed, err := service.Assess(
		ctx,
		"bob",
		knowledge.Assessment{
			Key:        "private-assess",
			ProposalID: draft.Proposal.ID,
			Version:    draft.Proposal.Version,
			Worthwhile: true,
		},
	)
	require.NoError(t, err)
	owner, err := service.Proposals(ctx, "bob", knowledge.ProposalQuery{})
	require.NoError(t, err)
	require.Len(t, owner, 1)
	require.Equal(t, "owner private draft canary", owner[0].Text)
	reviewer, err := service.Proposals(ctx, "alice", knowledge.ProposalQuery{ReviewQueue: true})
	require.NoError(t, err)
	require.Empty(t, reviewer)
	_, err = service.Execute(
		ctx,
		"alice",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "cannot-publish-private",
			ProposalID: draft.Proposal.ID,
			Version:    assessed.Proposal.Version,
			Decision:   "approve",
		},
	)
	require.Error(t, err)
	stillOwned, err := service.Proposals(ctx, "bob", knowledge.ProposalQuery{})
	require.NoError(t, err)
	require.Len(t, stillOwned, 1, "reader denial must not globally revoke the owner's draft")
}

func TestCausalReplayObservationIsTerminal(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	service := knowledge.Service{DB: f.db}
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
	command := knowledge.Command{
		Name:    knowledge.Curate,
		Key:     "replay-origin",
		Topic:   "travel",
		FactKey: "replay",
		Text:    "REPLAY-ORIGIN-SECRET",
	}
	_, err := service.ExecuteDerived(t.Context(), "bob", command, source)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review'`)
	require.NoError(t, err)
	denied, err := service.ExecuteDerived(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.True(t, denied.Redacted)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	replay, err := service.ExecuteDerived(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.True(t, replay.Redacted)
	encoded, err := json.Marshal(replay)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), command.Text)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='bob'`).Scan(&count),
	)
	require.Equal(t, 1, count, "replay must not create another committed effect")
}

func TestCausalReviewReplayRetainsRequestAuthority(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	service := knowledge.Service{DB: f.db}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
	)
	require.NoError(t, err)
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	draft, err := service.ExecuteDerived(
		t.Context(),
		"bob",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "replay-draft",
			Topic:   "travel",
			FactKey: "candidate",
			Text:    "proposal body",
		},
		source,
	)
	require.NoError(t, err)
	assessed, err := service.Assess(
		t.Context(),
		"bob",
		knowledge.Assessment{
			Key:        "assess-replay",
			ProposalID: draft.Proposal.ID,
			Version:    draft.Proposal.Version,
			Worthwhile: true,
		},
	)
	require.NoError(t, err)
	assessed = submitKnowledgeProposal(t, service, "bob", *assessed.Proposal)
	memo, err := service.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "review-note", FactKey: "diet", Text: "temporary"},
	)
	require.NoError(t, err)
	reviewSource := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.PrivateMemory}},
		},
	}
	command := knowledge.Command{
		Name:       knowledge.Review,
		Key:        "review-replay",
		ProposalID: draft.Proposal.ID,
		Version:    assessed.Proposal.Version,
		Decision:   "reject",
		Text:       "derived review reason",
	}
	_, err = service.ExecuteDerived(t.Context(), "alice", command, reviewSource)
	require.NoError(t, err)
	_, err = service.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoDelete, Key: "delete-note", FactKey: "diet", Version: memo.Memo.Version},
	)
	require.NoError(t, err)
	replay, err := service.ExecuteDerived(t.Context(), "alice", command, reviewSource)
	require.NoError(t, err)
	require.True(t, replay.Redacted)
	require.Empty(t, replay.Proposal.Text)
	require.Empty(t, replay.Proposal.Reason)
	var revoked bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT revoked FROM core.knowledge_proposal_authorities WHERE proposal_id=$1`, draft.Proposal.ID).
			Scan(&revoked),
	)
	require.False(t, revoked, "review request loss must not revoke the unrelated proposal origin")
	var receipts int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='alice'`).
			Scan(&receipts),
	)
	require.Equal(t, 3, receipts)
}
