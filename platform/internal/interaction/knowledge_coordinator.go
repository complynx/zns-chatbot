package interaction

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type KnowledgeClient interface {
	Memo(context.Context, string, string) (knowledge.Memo, error)
	KnowledgeProposals(context.Context, string, knowledge.ProposalQuery) ([]knowledge.Proposal, error)
	ExecuteKnowledge(context.Context, string, knowledge.Command) (knowledge.Result, error)
}

// KnowledgeHost is trusted application capability, never a model-selected credential.
type KnowledgeHost interface {
	ExecuteDerivedKnowledge(context.Context, string, knowledge.Command, readsource.Derivation) (knowledge.Result, error)
	AssessMemoryProposal(context.Context, string, knowledge.Assessment) (knowledge.Result, error)
	AttachMemorySources(context.Context, string, string, int64) error
	SubmitKnowledgeProposal(context.Context, string, knowledge.Submission) (knowledge.Result, error)
}

// KnowledgeCoordinator owns effect/assessment sequencing and original-source
// linkage. The host supplies the privacy-aware archival boundary; domains retain transactions.
type KnowledgeCoordinator struct {
	Client   KnowledgeClient
	Host     KnowledgeHost
	Store    Store
	Assessor KnowledgeAssessor
}

type KnowledgeOutcomeKind string

const (
	KnowledgeSaved            KnowledgeOutcomeKind = "saved"
	KnowledgeSuggested        KnowledgeOutcomeKind = "suggested"
	KnowledgeReviewTargetKind KnowledgeOutcomeKind = "review"
	knowledgeReviewAction                          = "review_card"
)

type KnowledgeOutcome struct {
	Result     knowledge.Result
	Refusal    *core.ProblemError
	Kind       KnowledgeOutcomeKind
	Assessment KnowledgeAssessmentState
}

type KnowledgeReviewTarget struct{ ProposalID int64 }

// Execute preserves a stored key. Only a new ordinary command receives the
// update-scoped default; replay still crosses the authenticated domain boundary.
func (c KnowledgeCoordinator) Execute(ctx context.Context, owner string, updateID int64,
	command knowledge.Command, source *readsource.Derivation) (KnowledgeOutcome, error) {
	if command.Name == knowledgeReviewAction {
		return KnowledgeOutcome{Kind: KnowledgeReviewTargetKind}, nil
	}
	if command.Key == "" {
		command.Key = "tg-knowledge-" + strconv.FormatInt(updateID, 10)
	}
	result, err := c.effect(ctx, owner, command, source)
	if err == nil {
		if attachErr := c.FinalizeCommand(ctx, owner, updateID, command, result); attachErr != nil {
			return KnowledgeOutcome{}, attachErr
		}
	}
	return c.finish(ctx, owner, updateID, command, result, err, false)
}

// FinalizeCommand completes original-source linkage after an authorized effect
// or receipt. It never executes the command or repeats model assessment.
func (c KnowledgeCoordinator) FinalizeCommand(ctx context.Context, owner string, updateID int64,
	command knowledge.Command, result knowledge.Result) error {
	if result.Redacted || command.Name == knowledge.Review || command.Name == knowledgeReviewAction {
		return nil
	}
	return c.AttachOriginalSources(ctx, owner, updateID, []string{command.Key})
}

// ExecuteScript keeps the durable script key and admitted derivation. Original
// source attachment runs after the host's privacy-aware request archival.
func (c KnowledgeCoordinator) ExecuteScript(ctx context.Context, owner string, updateID int64,
	command knowledge.Command, source *readsource.Derivation) (KnowledgeOutcome, error) {
	if source == nil || !source.Valid() {
		return KnowledgeOutcome{}, errors.New("missing admitted source")
	}
	result, err := c.effect(ctx, owner, command, source)
	return c.finish(ctx, owner, updateID, command, result, err, true)
}

func (c KnowledgeCoordinator) effect(ctx context.Context, owner string, command knowledge.Command,
	source *readsource.Derivation) (knowledge.Result, error) {
	if source == nil {
		return c.Client.ExecuteKnowledge(ctx, owner, command)
	}
	return c.Host.ExecuteDerivedKnowledge(ctx, owner, command, *source)
}

func (c KnowledgeCoordinator) finish(ctx context.Context, owner string, updateID int64, command knowledge.Command,
	result knowledge.Result, effectErr error, script bool) (KnowledgeOutcome, error) {
	outcome, err := knowledgeOutcome(ctx, result, effectErr)
	if err != nil || outcome.Refusal != nil {
		return outcome, err
	}
	if result.Proposal != nil && command.Name == knowledge.Suggest {
		if outcome.Assessment, err = c.assess(ctx, owner, updateID, *result.Proposal, script); err != nil {
			return KnowledgeOutcome{}, err
		}
		outcome.Kind = KnowledgeSuggested
	}
	return outcome, nil
}

func knowledgeOutcome(ctx context.Context, result knowledge.Result, err error) (KnowledgeOutcome, error) {
	if err == nil {
		return KnowledgeOutcome{Result: result, Kind: KnowledgeSaved}, nil
	}
	if ctx.Err() != nil {
		return KnowledgeOutcome{}, ctx.Err()
	}
	problem, ok := errors.AsType[*core.ProblemError](err)
	if !ok || problem.Status >= http.StatusInternalServerError {
		return KnowledgeOutcome{}, err
	}
	return KnowledgeOutcome{Refusal: problem}, nil
}

// Submit is separate from command execution. Its caller supplies the immutable
// owner-scoped manual button snapshot; the domain verifies and records consent.
func (c KnowledgeCoordinator) Submit(
	ctx context.Context,
	owner string,
	input knowledge.Submission,
) (KnowledgeOutcome, error) {
	result, err := c.Host.SubmitKnowledgeProposal(ctx, owner, input)
	return knowledgeOutcome(ctx, result, err)
}

// AttachOriginalSources resolves committed operations, including interrupted
// callbacks, without executing mutations or accepting caller-provided content.
func (c KnowledgeCoordinator) AttachOriginalSources(
	ctx context.Context,
	owner string,
	updateID int64,
	keys []string,
) error {
	for _, key := range keys {
		if err := c.Host.AttachMemorySources(ctx, owner, key, updateID); err != nil {
			return err
		}
	}
	return nil
}

func (c KnowledgeCoordinator) ReviewTarget(
	ctx context.Context,
	owner string,
	command knowledge.Command,
) (KnowledgeReviewTarget, error) {
	queue, err := c.Client.KnowledgeProposals(
		ctx,
		owner,
		knowledge.ProposalQuery{Event: command.Event, ReviewQueue: true, After: command.ProposalID + 1},
	)
	if err != nil {
		return KnowledgeReviewTarget{}, err
	}
	for _, proposal := range queue {
		if proposal.ID == command.ProposalID {
			return KnowledgeReviewTarget{ProposalID: proposal.ID}, nil
		}
	}
	return KnowledgeReviewTarget{}, errors.New("proposal changed; refresh review queue")
}
