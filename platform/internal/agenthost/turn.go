package agenthost

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

// Turn owns planning for one admitted input. TurnCoordinator remains the only
// persistence, winner and resume owner. All ports except Assets are required.
type Turn struct {
	Input     TurnInput
	State     TurnState
	Budget    QuestionBudget
	Initial   InitialContext
	Context   ContextBuilder
	Assets    AssetSource
	Model     ModelProvider
	Reads     ReadTools
	History   TurnHistory
	Knowledge TurnKnowledge
	AV        AVInspection
	Exposure  Exposure
	Binding   PlanBinding
}

var _ interaction.TurnHost = (*Turn)(nil)

func (h *Turn) Prepare(ctx context.Context) (conversation.Window, error) {
	if err := h.State.ValidateReply(ctx, h.Input.Owner, h.Input.UpdateID); err != nil {
		return conversation.Window{}, err
	}
	window, err := h.History.InitialHistory(ctx, h.Input.Owner)
	if err != nil {
		return conversation.Window{}, err
	}
	err = h.State.ValidateHistory(ctx, h.Input.Owner, h.Input.UpdateID, window.Generation)
	return window, err
}

func (h *Turn) Validate(ctx context.Context, plan interaction.SavedPlan) error {
	return h.State.ValidatePlan(ctx, h.Input.Owner, h.Input.UpdateID, plan)
}

func (h *Turn) ClearAV(ctx context.Context, ids []string) error {
	return h.State.ClearAV(ctx, h.Input.Owner, ids)
}

func (h *Turn) Plan(ctx context.Context, window conversation.Window) (interaction.SavedPlan, error) {
	allowed, remaining, err := h.Budget.ReserveQuestion(ctx, h.Input.Owner, h.Input.UpdateID)
	if err != nil {
		return interaction.SavedPlan{}, err
	}
	if !allowed {
		return h.Budget.QuotaNotice(ctx, h.Input.Owner)
	}
	input, err := h.buildInput(ctx, remaining, window)
	if err != nil {
		return interaction.SavedPlan{}, err
	}
	// Preserve the original spoken selection before visual refinement adds
	// speech and frames from an inspected recording.
	request := input
	plan, ids, err := h.planWithAV(ctx, &input)
	cached := interaction.SavedPlan{}
	if err == nil {
		err = h.Binding.Commands(ctx, h.Input.Owner, plan, input, request, &cached)
	}
	if err != nil {
		notice, propagate := h.Binding.Failure(err)
		if propagate || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return interaction.SavedPlan{}, err
		}
		text, translateErr := i18n.Translate(input.Language, notice, nil)
		if translateErr != nil {
			return interaction.SavedPlan{}, translateErr
		}
		plan = agent.Plan{View: input.View, Text: text}
		cached = interaction.SavedPlan{SystemNotice: notice}
	}
	h.cachePlan(&cached, plan, input, request, ids)
	return cached, nil
}

func (h *Turn) buildInput(ctx context.Context, remaining int, window conversation.Window) (agent.Input, error) {
	workflow, err := h.Initial.Current(ctx, h.Input.Owner)
	if err != nil {
		return agent.Input{}, err
	}
	catalog, err := h.Initial.Catalog(ctx, h.Input.Owner)
	if err != nil {
		return agent.Input{}, err
	}
	input := agent.Input{
		Text:                        h.Input.Text,
		Workflow:                    workflow,
		Catalog:                     catalog,
		AssistantQuestionsRemaining: &remaining,
	}
	if remaining < 0 {
		input.AssistantQuestionsRemaining = nil
	}
	if err = h.Initial.CurrentAV(ctx, &input); err != nil {
		return input, err
	}
	if err = h.Initial.Orders(ctx, h.Input.Owner, &input); err != nil {
		return input, err
	}
	if err = h.Initial.Profile(ctx, h.Input.Owner, &input); err != nil {
		return input, err
	}
	if err = h.Initial.Media(ctx, &input); err != nil {
		return input, err
	}
	err = h.Context.AddSupporting(ctx, AdmittedInput{Owner: h.Input.Owner, UpdateID: h.Input.UpdateID,
		TrustedPartnerIDs: h.Input.TrustedPartnerIDs, Projection: &input}, h.Assets, window)
	return input, err
}

func (h *Turn) cachePlan(cached *interaction.SavedPlan, plan agent.Plan, input, request agent.Input, ids []string) {
	if h.Input.MediaID != "" && input.AV == nil && plan.Action == nil && plan.OrderAction == nil &&
		plan.ProfileAction == nil && plan.KnowledgeAction == nil && plan.RegistrationAction == nil &&
		plan.View != agent.KnowledgeView && plan.View != agent.RegistrationView {
		plan.View = agent.MediaView
	}
	cached.Plan = plan
	cached.Version = input.Workflow.Version
	cached.ProfileVersion = request.Profile.Version
	if input.Profile != nil {
		cached.ProfileVersion = input.Profile.Version
	}
	cached.AVIDs = ids
	cached.MediaID = h.Input.MediaID
	cached.Plan.KnowledgeAction = nil
	cached.Plan.RegistrationAction = nil
	h.Binding.MediaSelection(cached, input, request)
	if proposal := plan.ProfileAction; proposal != nil {
		cached.ProfileCommand = &passes.Command{Name: proposal.Name, Field: proposal.Field, Value: proposal.Value,
			Version: cached.ProfileVersion, Origin: "agent"}
		// Private execution cache owns the value; it is not interaction history.
		cached.Plan.ProfileAction = nil
	}
	cached.PassAuthority = h.Exposure.Snapshot()
	cached.BindKind()
	if cached.SystemNotice == "" && cached.Kind != interaction.DerivedPlan {
		cached.Plan.Text = ""
	}
}
