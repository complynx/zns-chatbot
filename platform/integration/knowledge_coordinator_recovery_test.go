package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type interruptedKnowledgeHost struct {
	appclient.Host

	attachmentInterrupted bool
	assessmentInterrupted bool
}

func (h *interruptedKnowledgeHost) AttachMemorySources(ctx context.Context, owner, key string, updateID int64) error {
	if !h.attachmentInterrupted {
		h.attachmentInterrupted = true
		return errors.New("source attachment interrupted")
	}
	return h.Host.AttachMemorySources(ctx, owner, key, updateID)
}

func (h *interruptedKnowledgeHost) AssessMemoryProposal(
	ctx context.Context,
	owner string,
	input knowledge.Assessment,
) (knowledge.Result, error) {
	if !h.assessmentInterrupted {
		h.assessmentInterrupted = true
		return knowledge.Result{}, errors.New("assessment interrupted")
	}
	return h.Host.AssessMemoryProposal(ctx, owner, input)
}

func TestKnowledgeCoordinatorResumesAttachmentAndAssessment(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	const updateID int64 = 88791
	require.NoError(
		t,
		f.b.Host.ArchiveOriginal(ctx, "bob", "tg-user-88791", "user", "Remember this original coordinator request"),
	)
	host := &interruptedKnowledgeHost{Host: f.b.Host}
	model := &knowledgeModel{}
	coordinator := interaction.KnowledgeCoordinator{
		Client:   f.b.API,
		Host:     host,
		Store:    interaction.Store{DB: f.db},
		Assessor: completedKnowledgeAssessor(model),
	}
	command := knowledge.Command{
		Name:    knowledge.Suggest,
		Topic:   "travel",
		FactKey: "recovery",
		Text:    "A complete local synthetic knowledge suggestion.",
	}
	_, err := coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.EqualError(t, err, "source attachment interrupted")
	require.Zero(t, model.assessments)
	assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
	_, err = coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.EqualError(t, err, "assessment interrupted")
	require.Equal(t, 1, model.assessments)
	var verdict agent.KnowledgeAssessment
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='knowledge_assessment'`, updateID).
			Scan(&verdict),
	)
	require.True(t, verdict.Worthwhile)
	var links int
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM core.memory_proposal_sources`).Scan(&links))
	require.Equal(t, 1, links)
	restartedModel := &knowledgeModel{}
	coordinator.Assessor = completedKnowledgeAssessor(restartedModel)
	outcome, err := coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.NoError(t, err)
	require.Nil(t, outcome.Refusal)
	require.Equal(t, interaction.KnowledgeSuggested, outcome.Kind)
	require.Zero(t, restartedModel.assessments, "durable verdict wins after restart")
	assertKnowledgeRecoveryState(t, f, knowledge.AwaitingSubmission, 2)
	_, err = coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.NoError(t, err)
	require.Zero(t, restartedModel.assessments)
	assertKnowledgeRecoveryState(t, f, knowledge.AwaitingSubmission, 2)
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM core.memory_proposal_sources`).Scan(&links))
	require.Equal(t, 1, links, "retry retains exactly the original source link")
}

type knowledgeAssessorFunc func(context.Context, string, knowledge.Proposal) (interaction.KnowledgeAssessmentAttempt, error)

func (f knowledgeAssessorFunc) AssessKnowledge(
	ctx context.Context,
	owner string,
	proposal knowledge.Proposal,
) (interaction.KnowledgeAssessmentAttempt, error) {
	return f(ctx, owner, proposal)
}

func completedKnowledgeAssessor(model agent.KnowledgeClassifier) knowledgeAssessorFunc {
	return func(ctx context.Context, _ string, proposal knowledge.Proposal) (interaction.KnowledgeAssessmentAttempt, error) {
		verdict, err := model.AssessKnowledge(
			ctx,
			agent.KnowledgeAssessmentInput{
				Event:   proposal.Event,
				Topic:   proposal.Topic,
				FactKey: proposal.FactKey,
				Text:    proposal.Text,
			},
		)
		return interaction.KnowledgeAssessmentAttempt{
			Status:  interaction.KnowledgeAssessmentCompleted,
			Verdict: verdict,
		}, err
	}
}

func TestKnowledgeCoordinatorDeferredAssessmentKeepsPending(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	const updateID int64 = 88792
	require.NoError(
		t,
		f.b.Host.ArchiveOriginal(ctx, "bob", "tg-user-88792", "user", "Remember this deferred coordinator request"),
	)
	attempts := 0
	coordinator := interaction.KnowledgeCoordinator{
		Client: f.b.API,
		Host:   f.b.Host,
		Store:  interaction.Store{DB: f.db},
		Assessor: agenthost.KnowledgeAssessor{
			Authority: f.b.Host,
			Classifier: knowledgeClassifierFunc(
				func(ctx context.Context, input agent.KnowledgeAssessmentInput) (agent.KnowledgeAssessment, error) {
					attempts++
					if err := input.BeforeProvider(ctx); err != nil {
						return agent.KnowledgeAssessment{}, err
					}
					return agent.KnowledgeAssessment{}, errors.New("synthetic provider outage")
				},
			),
		},
	}

	command := knowledge.Command{
		Name:    knowledge.Suggest,
		Topic:   "travel",
		FactKey: "recovery",
		Text:    "A useful local synthetic suggestion waiting for assessment.",
	}
	outcome, err := coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.NoError(t, err)
	require.Equal(t, interaction.KnowledgeAssessmentDeferred, outcome.Assessment.Status)
	require.Equal(t, interaction.KnowledgeDeferredUnavailable, outcome.Assessment.Reason)
	require.Equal(t, 1, attempts)
	assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
	var receipts int
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='knowledge_assessment'", updateID).
			Scan(&receipts),
	)
	require.Zero(t, receipts, "deferred assessment is not a stored verdict")

	cancelledCtx, cancel := context.WithCancel(ctx)
	coordinator.Assessor = agenthost.KnowledgeAssessor{
		Authority: f.b.Host,
		Classifier: knowledgeClassifierFunc(
			func(context.Context, agent.KnowledgeAssessmentInput) (agent.KnowledgeAssessment, error) {
				cancel()
				return agent.KnowledgeAssessment{}, context.Canceled
			},
		),
	}
	_, err = coordinator.Execute(cancelledCtx, "bob", updateID, command, nil)
	require.ErrorIs(t, err, context.Canceled)
	cancel()
	assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='knowledge_assessment'", updateID).
			Scan(&receipts),
	)
	require.Zero(t, receipts)
	model := &knowledgeModel{}
	coordinator.Assessor = completedKnowledgeAssessor(model)
	outcome, err = coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.NoError(t, err)
	require.Equal(t, interaction.KnowledgeAssessmentCompleted, outcome.Assessment.Status)
	require.Equal(t, 1, model.assessments)
	assertKnowledgeRecoveryState(t, f, knowledge.AwaitingSubmission, 2)
	coordinator.Assessor = knowledgeAssessorFunc(
		func(context.Context, string, knowledge.Proposal) (interaction.KnowledgeAssessmentAttempt, error) {
			return interaction.KnowledgeAssessmentAttempt{}, errors.New(
				"stored winner must avoid another provider request",
			)
		},
	)
	_, err = coordinator.Execute(ctx, "bob", updateID, command, nil)
	require.NoError(t, err)
	assertKnowledgeRecoveryState(t, f, knowledge.AwaitingSubmission, 2)
}

type knowledgeClassifierFunc func(context.Context, agent.KnowledgeAssessmentInput) (agent.KnowledgeAssessment, error)

func (f knowledgeClassifierFunc) AssessKnowledge(
	ctx context.Context,
	input agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	return f(ctx, input)
}
