package agenthost_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

// Unused embedded ports panic if planning starts performing an unexpected leaf
// or winner/cleanup operation. Tests implement only the selected path.
type turnFixture struct {
	agenthost.TurnState
	agenthost.QuestionBudget
	agenthost.InitialContext
	agenthost.ReadTools
	agenthost.TurnHistory
	agenthost.TurnKnowledge
	agenthost.PlanBinding

	plans           []agent.Plan
	scopes          []agent.RequestScope
	calls           []string
	exposures       int
	rejectExposure  int
	resets          int
	allowed         bool
	historyFailure  error
	observedRequest agent.Input
	observedFinal   agent.Input
}

func (f *turnFixture) ReserveQuestion(context.Context, string, int64) (bool, int, error) {
	f.calls = append(f.calls, "quota")
	return f.allowed, 2, nil
}
func (f *turnFixture) QuotaNotice(context.Context, string) (interaction.SavedPlan, error) {
	f.calls = append(f.calls, "quota-notice")
	return interaction.SavedPlan{SystemNotice: i18n.AgentQuotaReached}, nil
}
func (f *turnFixture) Current(context.Context, string) (workflow.Workflow, error) {
	f.calls = append(f.calls, "current")
	return workflow.Workflow{Version: 3}, nil
}
func (f *turnFixture) Catalog(context.Context, string) ([]workflow.Slot, error) { return nil, nil }
func (f *turnFixture) CurrentAV(_ context.Context, input *agent.Input) error {
	input.AV = &agent.AVContext{ID: "original-voice", Kind: "voice"}
	return nil
}
func (f *turnFixture) Orders(context.Context, string, *agent.Input) error { return nil }
func (f *turnFixture) Profile(_ context.Context, _ string, input *agent.Input) error {
	input.Profile = &agent.ProfileContext{Version: 7}
	return nil
}
func (f *turnFixture) Media(context.Context, *agent.Input) error { return nil }
func (f *turnFixture) Settings(ctx context.Context, _ string) (context.Context, error) {
	return ctx, nil
}
func (f *turnFixture) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	scope, _ := agent.RequestScopeFromContext(ctx)
	f.scopes = append(f.scopes, scope)
	f.calls = append(f.calls, "model")
	// Simulate selection and answer requests made by one provider invocation.
	for range 2 {
		if err := input.BeforeProvider(ctx, &input); err != nil {
			return agent.Plan{}, err
		}
	}
	plan := f.plans[len(f.scopes)-1]
	return plan, nil
}
func (f *turnFixture) CheckHistoryGeneration(context.Context, string, int64) error {
	f.calls = append(f.calls, "generation")
	return f.historyFailure
}
func (f *turnFixture) Inspection(context.Context, string, int64) (*agent.AVInspectionContext, error) {
	return &agent.AVInspectionContext{Remaining: 2}, nil
}

func (f *turnFixture) Refine(
	_ context.Context,
	_ string,
	_ int64,
	p agent.MediaProposal,
	input *agent.Input,
) (i18n.ID, error) {
	f.calls = append(f.calls, "refine")
	input.Frames = append(input.Frames, agent.Attachment{})
	input.AVInspection.Remaining--
	input.AVInspection.Completed = append(input.AVInspection.Completed, agent.AVInspection{MediaID: p.MediaID})
	return "", nil
}
func (f *turnFixture) Lineup(_ agent.LineupQuery, _ *agent.Input) error {
	f.calls = append(f.calls, "lineup")
	return nil
}

func (f *turnFixture) ReadHistory(context.Context, string, int64, agent.HistoryProposal, *agent.Input) error {
	f.calls = append(f.calls, "history")
	return nil
}
func (f *turnFixture) Reset() { f.resets++ }
func (f *turnFixture) Expose(context.Context, *agent.Input) error {
	f.exposures++
	if f.rejectExposure > 0 && f.exposures == f.rejectExposure {
		return context.Canceled
	}
	return nil
}
func (f *turnFixture) Snapshot() *interaction.PlanAuthority {
	return &interaction.PlanAuthority{Reads: []interaction.PassContextDependency{}}
}
func (f *turnFixture) Validate(context.Context, interaction.SavedPlan) error {
	f.calls = append(f.calls, "validate")
	return nil
}

func (f *turnFixture) Commands(
	_ context.Context,
	_ string,
	_ agent.Plan,
	input, request agent.Input,
	_ *interaction.SavedPlan,
) error {
	f.calls = append(f.calls, "bind")
	f.observedRequest = request
	f.observedFinal = input
	return nil
}
func (f *turnFixture) MediaSelection(*interaction.SavedPlan, agent.Input, agent.Input) {}
func (f *turnFixture) Failure(err error) (i18n.ID, bool) {
	return i18n.AgentUnavailable, errors.Is(err, f.historyFailure) && f.historyFailure != nil
}

func newTestTurn(f *turnFixture) *agenthost.Turn {
	sources := &contextFixture{}
	return &agenthost.Turn{
		Input: agenthost.TurnInput{
			Owner:    "owner",
			UpdateID: 42,
			Text:     "original utterance",
			MediaID:  "original-voice",
		},
		State:   f,
		Budget:  f,
		Initial: f,
		Context: agenthost.ContextBuilder{
			Sources:     sources,
			History:     sources,
			Knowledge:   sources,
			Reads:       sources,
			PendingFood: sources,
		},
		Model:     f,
		Reads:     f,
		History:   f,
		Knowledge: f,
		AV:        f,
		Exposure:  f,
		Binding:   f,
	}
}

func TestTurnLoopsReadsAndAVBeforeBinding(t *testing.T) {
	t.Parallel()
	f := &turnFixture{allowed: true, plans: []agent.Plan{
		{LineupAction: &agent.LineupQuery{Scope: "full"}},
		{HistoryAction: &agent.HistoryProposal{}},
		{MediaAction: &agent.MediaProposal{Intent: "inspect_video", MediaID: "inspected-video"}},
		{Text: "answer"},
	}}
	result, err := newTestTurn(f).Plan(t.Context(), conversation.Window{})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]string{
			"quota",
			"current",
			"model",
			"generation",
			"lineup",
			"model",
			"generation",
			"history",
			"model",
			"generation",
			"validate",
			"refine",
			"model",
			"generation",
			"validate",
			"bind",
		},
		f.calls,
	)
	assert.Equal(t, 12, f.exposures)
	assert.Equal(t, 4, f.resets)
	assert.Equal(
		t,
		[]agent.RequestScope{
			{Owner: "owner", UpdateID: 42, Turn: 0},
			{Owner: "owner", UpdateID: 42, Turn: 1},
			{Owner: "owner", UpdateID: 42, Turn: 2},
			{Owner: "owner", UpdateID: 42, Turn: 3},
		},
		f.scopes,
	)
	assert.Equal(t, "original utterance", f.observedRequest.Text)
	assert.Equal(t, "original-voice", f.observedRequest.AV.ID)
	assert.Empty(t, f.observedRequest.Frames)
	assert.Len(t, f.observedFinal.Frames, 1)
	assert.Equal(t, []string{"original-voice", "inspected-video"}, result.AVIDs)
	assert.Equal(t, int64(3), result.Version)
	assert.Equal(t, int64(7), result.ProfileVersion)
}

func TestTurnProviderExposureFailureCannotBindOrRead(t *testing.T) {
	t.Parallel()
	f := &turnFixture{allowed: true, rejectExposure: 3}
	_, err := newTestTurn(f).Plan(t.Context(), conversation.Window{})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"quota", "current", "model"}, f.calls)
	assert.Equal(t, 3, f.exposures)
}

func TestTurnHistoryFenceStopsReadAndBinding(t *testing.T) {
	t.Parallel()
	failure := errors.New("generation changed")
	f := &turnFixture{
		allowed:        true,
		historyFailure: failure,
		plans:          []agent.Plan{{HistoryAction: &agent.HistoryProposal{}}},
	}
	_, err := newTestTurn(f).Plan(t.Context(), conversation.Window{})
	require.ErrorIs(t, err, failure)
	assert.Equal(t, []string{"quota", "current", "model", "generation"}, f.calls)
}

func TestTurnQuotaDenialDoesNotAssembleOrInvokeModel(t *testing.T) {
	t.Parallel()
	f := &turnFixture{}
	result, err := newTestTurn(f).Plan(t.Context(), conversation.Window{})
	require.NoError(t, err)
	assert.Equal(t, i18n.AgentQuotaReached, result.SystemNotice)
	assert.Equal(t, []string{"quota", "quota-notice"}, f.calls)
}

type preparationFixture struct {
	agenthost.TurnState
	agenthost.TurnHistory

	calls []string
	fail  string
}

func (f *preparationFixture) step(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errContextRead
	}
	return nil
}
func (f *preparationFixture) ValidateReply(context.Context, string, int64) error {
	return f.step("reply")
}
func (f *preparationFixture) InitialHistory(context.Context, string) (conversation.Window, error) {
	return conversation.Window{Generation: 17}, f.step("window")
}
func (f *preparationFixture) ValidateHistory(_ context.Context, _ string, _ int64, generation int64) error {
	if generation != 17 {
		return errors.New("wrong history generation")
	}
	return f.step("interactions")
}
func TestTurnPreparationValidatesBeforePlanning(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		fail  string
		calls []string
	}{
		{name: "ready", calls: []string{"reply", "window", "interactions"}},
		{name: "reply-revoked", fail: "reply", calls: []string{"reply"}},
		{name: "window-unavailable", fail: "window", calls: []string{"reply", "window"}},
		{name: "interactions-stale", fail: "interactions", calls: []string{"reply", "window", "interactions"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := &preparationFixture{fail: test.fail}
			host := agenthost.Turn{Input: agenthost.TurnInput{Owner: "owner", UpdateID: 42}, State: f, History: f}
			window, err := host.Prepare(t.Context())
			if test.fail == "" {
				require.NoError(t, err)
				assert.Equal(t, int64(17), window.Generation)
			} else {
				require.ErrorIs(t, err, errContextRead)
			}
			assert.Equal(t, test.calls, f.calls)
		})
	}
}
