package integration_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestKnowledgePublicationKeepsPrivateClosureServerSide(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	ctx := t.Context()
	generation := int64(0)
	const privateKey = "alice-private-source-identifier"
	_, err := s.ExecuteDerived(
		ctx,
		"alice",
		knowledge.Command{
			Name:    knowledge.MemoSet,
			Key:     "private-memo",
			FactKey: privateKey,
			Text:    "PRIVATE SOURCE BODY",
		},
		readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
	)
	require.NoError(t, err)
	memo, err := s.Memo(ctx, "alice", privateKey)
	require.NoError(t, err)
	parent, err := s.ExecuteDerived(
		ctx,
		"alice",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "parent",
			Event:   "kb-current",
			Topic:   "travel",
			FactKey: "parent",
			Text:    "Consented self-contained body.",
		},
		readsource.Derivation{Generation: &generation, PrivateHistory: true, Authorities: memo.ReadAuthorities},
	)
	require.NoError(t, err)
	parent, err = s.Assess(
		ctx,
		"alice",
		knowledge.Assessment{
			Key:        "parent-assess",
			ProposalID: parent.Proposal.ID,
			Version:    parent.Proposal.Version,
			Worthwhile: true,
		},
	)
	require.NoError(t, err)
	parent = submitKnowledgeProposal(t, s, "alice", *parent.Proposal)
	require.True(t, parent.Proposal.Submitted)
	queue, err := s.Proposals(ctx, "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, queue, 1)
	childCommand := knowledge.Command{
		Name:    knowledge.Suggest,
		Key:     "child",
		Event:   "kb-current",
		Topic:   "travel",
		FactKey: "child",
		Text:    "Child from consented body.",
	}
	childSource := readsource.Derivation{Generation: &generation, Authorities: queue[0].ReadAuthorities}
	child, err := s.ExecuteDerived(ctx, "bob", childCommand, childSource)
	require.NoError(t, err)
	check := func(label string, value any) {
		t.Helper()
		data, e := json.Marshal(value)
		require.NoError(t, e)
		assert.NotContains(t, string(data), privateKey, label)
		assert.NotContains(t, string(data), "PRIVATE SOURCE BODY", label)
		assert.NotContains(t, string(data), `"causal"`, label)
	}
	check("new child result", child)
	// An older receipt may still contain expanded provenance. Authorization
	// retains that evidence internally while the returned result is projected.
	_, err = s.DB.Exec(
		ctx,
		`UPDATE core.knowledge_operations o SET result=jsonb_set(result,'{proposal,read_authorities}',a.authorities||(result#>'{proposal,read_authorities}')) FROM core.knowledge_proposal_authorities a WHERE o.actor='bob' AND (o.result->'proposal'->>'id')::bigint=a.proposal_id AND a.proposal_id=$1`,
		child.Proposal.ID,
	)
	require.NoError(t, err)
	receipt, found, err := s.CommandReceipt(ctx, "bob", childCommand, childSource)
	require.NoError(t, err)
	require.True(t, found)
	check("child receipt", receipt)
	children, err := s.Proposals(ctx, "bob", knowledge.ProposalQuery{Event: "kb-current"})
	require.NoError(t, err)
	check("own child list", children)
	child, err = s.Assess(
		ctx,
		"bob",
		knowledge.Assessment{
			Key:        "child-assess",
			ProposalID: child.Proposal.ID,
			Version:    child.Proposal.Version,
			Worthwhile: true,
		},
	)
	require.NoError(t, err)
	check("child assessment", child)
	child = submitKnowledgeProposal(t, s, "bob", *child.Proposal)
	approval := knowledge.Command{
		Name:       knowledge.Review,
		Key:        "approve-child",
		Event:      "kb-current",
		ProposalID: child.Proposal.ID,
		Version:    child.Proposal.Version,
		Decision:   "approve",
	}
	published, err := s.Execute(ctx, "kbadmin", approval)
	require.NoError(t, err)
	check("approved fact result", published)
	replay, err := s.Execute(ctx, "kbadmin", approval)
	require.NoError(t, err)
	check("approval receipt", replay)
	fact, err := s.Fact(ctx, "visitor", "kb-current", "travel", "child")
	require.NoError(t, err)
	require.Equal(t, "Child from consented body.", fact.Text)
	check("ordinary reader fact", fact)
	page, err := s.Retrieve(ctx, "visitor", knowledge.Query{Event: "kb-current"})
	require.NoError(t, err)
	check("fact collection", page)
	copied, err := s.ExecuteDerived(
		ctx,
		"visitor",
		knowledge.Command{Name: knowledge.MemoSet, Key: "copy-fact", FactKey: "public-copy", Text: fact.Text},
		readsource.Derivation{Generation: &generation, Authorities: fact.ReadAuthorities},
	)
	require.NoError(t, err)
	check("fact-derived private copy", copied)
	// A cached public opaque reference must retain its live server closure even
	// before another reader has lazily visited/revoked the original proposal.
	tx, err := s.DB.Begin(ctx)
	require.NoError(t, err)
	valid, err := readsource.Lock(ctx, tx, "visitor", fact.ReadAuthorities)
	require.NoError(t, err)
	require.NotContains(t, valid, false)
	require.NoError(t, tx.Rollback(ctx))
	_, err = s.Execute(
		ctx,
		"alice",
		knowledge.Command{Name: knowledge.MemoDelete, Key: "retire-source", FactKey: privateKey, Version: memo.Version},
	)
	require.NoError(t, err)
	tx, err = s.DB.Begin(ctx)
	require.NoError(t, err)
	valid, err = readsource.Lock(ctx, tx, "visitor", fact.ReadAuthorities)
	require.NoError(t, err)
	require.Contains(t, valid, false)
	require.NoError(t, tx.Rollback(ctx))
	_, err = s.Fact(ctx, "visitor", "kb-current", "travel", "child")
	require.Error(t, err)
	_, err = s.Memo(ctx, "visitor", "public-copy")
	require.Error(t, err)
	receipt, found, err = s.CommandReceipt(ctx, "bob", childCommand, childSource)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, receipt.Redacted)
	check("retired receipt", receipt)
	var retained bool
	require.NoError(
		t,
		s.DB.QueryRow(ctx, `SELECT authorities::text LIKE '%' || $1 || '%' FROM core.knowledge_proposal_authorities WHERE proposal_id=$2`, privateKey, child.Proposal.ID).
			Scan(&retained),
	)
	require.True(t, retained, "private provenance remains server-side")
}

func TestKnowledgeOpaqueMemoryRejectsInvalidAncestry(t *testing.T) {
	t.Parallel()
	for _, forward := range []bool{false, true} {
		t.Run(strconv.FormatBool(forward), func(t *testing.T) {
			t.Parallel()
			s := knowledgeFixture(t)
			ctx := t.Context()
			generation := int64(0)
			first, err := s.ExecuteDerived(
				ctx,
				"alice",
				knowledge.Command{Name: knowledge.MemoSet, Key: "first", FactKey: "first", Text: "first"},
				readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
			)
			require.NoError(t, err)
			ancestor := first.Memo.ReadAuthorities
			if forward {
				second, writeErr := s.ExecuteDerived(
					ctx,
					"alice",
					knowledge.Command{Name: knowledge.MemoSet, Key: "second", FactKey: "second", Text: "second"},
					readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
				)
				require.NoError(t, writeErr)
				ancestor = second.Memo.ReadAuthorities
			}
			malformed := []readsource.Authority{
				{Causal: &readsource.CausalSource{Actor: "alice", Generation: &generation, Authorities: ancestor}},
			}
			_, err = s.DB.Exec(
				ctx,
				`UPDATE core.memory_read_authorities SET authorities=$1 WHERE owner='alice' AND item_key='first'`,
				malformed,
			)
			require.NoError(t, err)
			tx, err := s.DB.Begin(ctx)
			require.NoError(t, err)
			_, err = readsource.Lock(ctx, tx, "alice", first.Memo.ReadAuthorities)
			require.ErrorIs(t, err, readsource.ErrLimit)
			require.NoError(t, tx.Rollback(ctx))
		})
	}
}
