package agenthost

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type knowledgePreparationFixture struct {
	KnowledgeDomain
	interaction.KnowledgeClient
	scope                         knowledge.Scope
	state                         knowledge.MemoryDeletionState
	scopeErr, stateErr, reviewErr error
	memo                          knowledge.Memo
	calls                         []string
}

func (f *knowledgePreparationFixture) KnowledgeScope(
	_ context.Context, owner, event string,
) (knowledge.Scope, error) {
	f.calls = append(f.calls, "scope:"+owner+":"+event)
	return f.scope, f.scopeErr
}
func (f *knowledgePreparationFixture) MemoryDeletions(
	_ context.Context, owner string,
) (knowledge.MemoryDeletionState, error) {
	f.calls = append(f.calls, "deletions:"+owner)
	return f.state, f.stateErr
}
func (f *knowledgePreparationFixture) KnowledgeProposals(
	_ context.Context, owner string, query knowledge.ProposalQuery,
) ([]knowledge.Proposal, error) {
	f.calls = append(f.calls, "review:"+owner+":"+query.Event)
	return []knowledge.Proposal{{ID: 999}}, f.reviewErr
}
func (f *knowledgePreparationFixture) Memo(_ context.Context, owner, key string) (knowledge.Memo, error) {
	f.calls = append(f.calls, "memo:"+owner+":"+key)
	return f.memo, nil
}
func (f *knowledgePreparationFixture) preparation() KnowledgeScriptPreparation {
	return KnowledgeScriptPreparation{Domain: f, Reader: KnowledgeReader{Domain: f},
		Coordinator: interaction.KnowledgeCoordinator{Client: f}}
}

func TestKnowledgePreparationReadSnapshotsWithoutMutationSource(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		proposal agent.KnowledgeProposal
	}{
		{name: "knowledge.read", proposal: agent.KnowledgeProposal{Name: agent.KnowledgeRead}},
		{name: "knowledge.scopes"},
		{name: "knowledge.memos"},
		{name: "knowledge.review_queue", proposal: agent.KnowledgeProposal{
			Name: agent.KnowledgeProposals, Event: "event", ReviewQueue: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := &knowledgePreparationFixture{state: knowledge.MemoryDeletionState{PrivateGeneration: 7}}
			record, err := fixture.preparation().Prepare(t.Context(), "owner", test.name,
				test.proposal, agent.Input{}, false)
			require.NoError(t, err)
			require.Equal(t, []string{"deletions:owner"}, fixture.calls)
			require.Equal(t, &fixture.state, record.MemoryReadState)
			require.Nil(t, record.Memory)
			require.Equal(t, scriptInterrupted, record.Outcome.Error)
			if test.proposal.ReviewQueue {
				require.NotNil(t, record.KnowledgeRead)
				require.Equal(t, "event", record.KnowledgeRead.Scope)
			} else {
				require.Nil(t, record.KnowledgeRead)
			}
		})
	}
}

func TestKnowledgePreparationCurrentRightsAndObservedVersions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                               string
		bound, curate, deleted, historical bool
		failure                            error
		failureAt                          string
		allowed                            bool
	}{
		{name: "current-observed", bound: true, curate: true, allowed: true},
		{name: "unbound", curate: true},
		{name: "revoked", bound: true},
		{name: "deleted-read", bound: true, curate: true, deleted: true},
		{name: "historical-read", bound: true, curate: true, historical: true},
		{name: "scope-canceled", bound: true, curate: true, failureAt: "scope", failure: context.Canceled},
		{name: "state-sql", bound: true, curate: true, failureAt: "state",
			failure: core.DatabaseFailure(&core.ProblemError{Status: 404, Code: "missing"})},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := &knowledgePreparationFixture{scope: knowledge.Scope{Event: "event", CanCurate: test.curate}}
			if test.deleted {
				fixture.state.PrivateGeneration = 7
			}
			if test.failureAt == "scope" {
				fixture.scopeErr = test.failure
			} else if test.failureAt == "state" {
				fixture.stateErr = test.failure
			}
			input := agent.Input{Knowledge: &agent.KnowledgeContext{
				Scopes: []knowledge.Scope{{Event: "event", CanCurate: true}}, Remaining: 2,
				Reads: []agent.KnowledgeReadResult{{Facts: []knowledge.Fact{
					{Event: "event", Topic: "topic", Key: "key", Version: 9, HistoricalFallback: test.historical},
				}}},
			}}
			proposal := agent.KnowledgeProposal{Name: knowledge.Curate, Event: "event", Topic: "topic",
				FactKey: "key", Text: "new text"}
			record, err := fixture.preparation().Prepare(t.Context(), "owner", "knowledge.curate",
				proposal, input, test.bound)
			if test.allowed {
				require.NoError(t, err)
				require.Equal(t, int64(9), record.Memory.Version)
				require.Equal(t, "new text", record.Memory.Text)
				require.Empty(t, record.Memory.Key, "host key binding remains at the existing admission boundary")
			} else {
				require.Error(t, err)
				require.Nil(t, record.Memory)
				if test.failure != nil {
					require.ErrorIs(t, err, test.failure)
				}
			}
			require.Equal(t, 2, input.Knowledge.Remaining)
			switch {
			case !test.bound:
				require.Empty(t, fixture.calls)
			case test.failureAt == "scope":
				require.Equal(t, []string{"scope:owner:event"}, fixture.calls)
			default:
				require.Equal(t, []string{"scope:owner:event", "deletions:owner"}, fixture.calls)
			}
		})
	}
}

func TestKnowledgePreparationReviewRetainsOnlyObservedProjection(t *testing.T) {
	t.Parallel()
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "allowed", true: "denied"}[denied], func(t *testing.T) {
			t.Parallel()
			fixture := &knowledgePreparationFixture{scope: knowledge.Scope{Event: "event", CanReview: true}}
			if denied {
				fixture.reviewErr = &core.ProblemError{Status: 403, Code: "forbidden"}
			}
			input := agent.Input{Knowledge: &agent.KnowledgeContext{Reads: []agent.KnowledgeReadResult{{
				Request:   agent.KnowledgeProposal{Name: agent.KnowledgeProposals, Event: "event", ReviewQueue: true},
				Proposals: []knowledge.Proposal{{ID: 7, Event: "event"}},
			}}}}
			record, err := fixture.preparation().Prepare(t.Context(), "owner", "knowledge.review_card",
				agent.KnowledgeProposal{Name: "review_card", Event: "event", ProposalID: 7}, input, true)
			if denied {
				require.Error(t, err)
				require.Nil(t, record.Memory)
				require.Empty(t, input.Knowledge.Reads[0].Proposals)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(7), record.Memory.ProposalID)
				require.Equal(t, "review_card", record.Memory.Name)
				require.Equal(t, int64(7), input.Knowledge.Reads[0].Proposals[0].ID)
			}
			require.Equal(t, []string{"scope:owner:event", "deletions:owner", "review:owner:event"}, fixture.calls)
		})
	}
}

func TestKnowledgePreparationMemoFallbackUsesCurrentInactiveVersion(t *testing.T) {
	t.Parallel()
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive", true: "active"}[active], func(t *testing.T) {
			t.Parallel()
			fixture := &knowledgePreparationFixture{memo: knowledge.Memo{Key: "diet", Version: 4, Active: active}}
			record, err := fixture.preparation().Prepare(t.Context(), "owner", "knowledge.memo_set",
				agent.KnowledgeProposal{
					Name:    knowledge.MemoSet,
					FactKey: "diet",
					Text:    "preference",
				}, agent.Input{}, true)
			if active {
				require.Error(t, err)
				require.Nil(t, record.Memory)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(4), record.Memory.Version)
			}
			require.Equal(t, []string{"scope:owner:", "deletions:owner", "memo:owner:diet"}, fixture.calls)
		})
	}
}

func TestKnowledgePreparationReadFailureKeepsInterruptedAdmission(t *testing.T) {
	t.Parallel()
	failure := errors.Join(context.Canceled, core.DatabaseFailure(errors.New("sql")))
	fixture := &knowledgePreparationFixture{stateErr: failure}
	record, err := fixture.preparation().Prepare(t.Context(), "owner", "knowledge.read",
		agent.KnowledgeProposal{Name: agent.KnowledgeRead}, agent.Input{}, false)
	require.ErrorIs(t, err, failure)
	require.Nil(t, record.MemoryReadState)
	require.Nil(t, record.Memory)
	require.Equal(t, scriptInterrupted, record.Outcome.Error)
	require.Equal(t, []string{"deletions:owner"}, fixture.calls)
}
