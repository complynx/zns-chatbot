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
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

var errContextRead = errors.New("context read unavailable")

type contextFixture struct {
	calls    []string
	fail     string
	cancel   context.CancelFunc
	partners []int64
	scriptID int64
}

func (f *contextFixture) step(ctx context.Context, name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		if f.cancel != nil {
			f.cancel()
			return ctx.Err()
		}
		return errContextRead
	}
	return ctx.Err()
}

func (f *contextFixture) Read(ctx context.Context, _ string) (legacyfood.Command, bool, error) {
	return legacyfood.Command{}, false, f.step(ctx, "food")
}
func (f *contextFixture) FoodView(context.Context, string, string, string) (legacyfood.View, error) {
	return legacyfood.View{}, errContextRead
}
func (f *contextFixture) BusinessCapabilities(ctx context.Context, _, _ string) (agent.BusinessCapabilities, error) {
	return agent.BusinessCapabilities{}, f.step(ctx, "business")
}
func (f *contextFixture) Describe(ctx context.Context) (agent.AssetContext, error) {
	return agent.AssetContext{}, f.step(ctx, "assets")
}
func (f *contextFixture) Knowledge(ctx context.Context, _ string, _ int64) (*agent.KnowledgeContext, error) {
	return &agent.KnowledgeContext{Remaining: 0, Omitted: true}, f.step(ctx, "knowledge")
}
func (f *contextFixture) RefreshKnowledge(ctx context.Context, _ string, _ *agent.KnowledgeContext) error {
	return f.step(ctx, "knowledge")
}
func (f *contextFixture) Registration(
	ctx context.Context, _ string, _ int64, partners []int64,
) (*agent.RegistrationContext, error) {
	f.partners = partners
	return &agent.RegistrationContext{Remaining: 0}, f.step(ctx, "registration")
}
func (f *contextFixture) RefreshRegistration(ctx context.Context, _ string, _ *agent.RegistrationContext) error {
	return f.step(ctx, "registration")
}
func (f *contextFixture) Script(ctx context.Context, _ string, id int64) (*agent.ScriptContext, error) {
	f.scriptID = id
	return &agent.ScriptContext{UpdateID: id, Remaining: 0}, f.step(ctx, "script")
}
func (f *contextFixture) History(
	ctx context.Context, _ string, _ int64, _ *agent.Input, _ conversation.Window,
) error {
	return f.step(ctx, "history")
}
func (f *contextFixture) RefreshHistory(ctx context.Context, _ string, _ *agent.Input) error {
	return f.step(ctx, "history")
}

func TestSupportingContextPreservesAdmissionAndBudgets(t *testing.T) {
	t.Parallel()
	f := &contextFixture{}
	builder := agenthost.ContextBuilder{Sources: f, History: f, Knowledge: f, Reads: f, PendingFood: f}
	av := &agent.AVContext{ID: "owned-intake"}
	input := agent.Input{Text: "original caption", AV: av}
	admitted := agenthost.AdmittedInput{
		Owner:             "owner",
		UpdateID:          42,
		TrustedPartnerIDs: []int64{77},
		Projection:        &input,
	}
	require.NoError(t, builder.AddSupporting(t.Context(), admitted, f, conversation.Window{}))
	assert.Equal(t, []string{"food", "assets", "knowledge", "script", "registration", "history"}, f.calls)
	assert.Equal(t, []int64{77}, f.partners)
	assert.Equal(t, "original caption", input.Text)
	assert.Same(t, av, input.AV)
	assert.Zero(t, input.Knowledge.Remaining)
	assert.True(t, input.Knowledge.Omitted)
	assert.Zero(t, input.Registration.Remaining)
	assert.Zero(t, input.Script.Remaining)
}

func TestSupportingContextStopsBeforeLaterReads(t *testing.T) {
	t.Parallel()
	f := &contextFixture{fail: "knowledge"}
	builder := agenthost.ContextBuilder{Sources: f, History: f, Knowledge: f, Reads: f, PendingFood: f}
	input := agent.Input{}
	err := builder.AddSupporting(t.Context(), agenthost.AdmittedInput{Projection: &input}, f, conversation.Window{})
	require.ErrorIs(t, err, errContextRead)
	assert.Equal(t, []string{"food", "assets", "knowledge"}, f.calls)
}

func TestAssetsFailureIsEvidenceButCancellationStopsAssembly(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "canceled"}[canceled], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &contextFixture{fail: "assets"}
			if canceled {
				f.cancel = cancel
			}
			builder := agenthost.ContextBuilder{Sources: f, History: f, Knowledge: f, Reads: f, PendingFood: f}
			input := agent.Input{Text: "original"}
			err := builder.AddSupporting(ctx, agenthost.AdmittedInput{Projection: &input}, f, conversation.Window{})
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, []string{"food", "assets"}, f.calls)
			} else {
				require.NoError(t, err)
				require.NotNil(t, input.Assets)
				assert.Equal(t, "unavailable", input.Assets.Items[0].Status)
			}
			assert.Equal(t, "original", input.Text)
		})
	}
}

func TestRefreshUsesCurrentScriptIdentityWithoutRestoringBudget(t *testing.T) {
	t.Parallel()
	for _, withScript := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-script", true: "saved-script"}[withScript], func(t *testing.T) {
			t.Parallel()
			f := &contextFixture{}
			builder := agenthost.ContextBuilder{Sources: f, History: f, Knowledge: f, Reads: f, PendingFood: f}
			input := agent.Input{Knowledge: &agent.KnowledgeContext{Remaining: 0, Omitted: true}}
			expected := []string{"food", "business", "knowledge", "registration", "history"}
			if withScript {
				input.Script = &agent.ScriptContext{UpdateID: 57, Remaining: 0}
				expected = []string{"food", "script", "business", "knowledge", "registration", "history"}
			}
			require.NoError(t, builder.Refresh(t.Context(), agenthost.AdmittedInput{UpdateID: 99, Projection: &input}))
			assert.Equal(t, expected, f.calls)
			assert.True(t, input.Knowledge.Omitted)
			assert.Zero(t, input.Knowledge.Remaining)
			if withScript {
				assert.Equal(t, int64(57), f.scriptID)
				assert.Zero(t, input.Script.Remaining)
			}
		})
	}
}

func TestRefreshFailureStopsProviderPreparation(t *testing.T) {
	t.Parallel()
	f := &contextFixture{fail: "business"}
	builder := agenthost.ContextBuilder{Sources: f, History: f, Knowledge: f, Reads: f, PendingFood: f}
	input := agent.Input{}
	require.ErrorIs(t, builder.Refresh(t.Context(), agenthost.AdmittedInput{Projection: &input}), errContextRead)
	assert.Equal(t, []string{"food", "business"}, f.calls)
}
