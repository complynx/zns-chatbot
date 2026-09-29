package interaction_test

import (
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func publicPlan() interaction.SavedPlan {
	return interaction.SavedPlan{FormatVersion: interaction.CurrentFormatVersion,
		Kind:  interaction.DerivedPlan,
		State: interaction.Ready,
		Plan:  agent.Plan{Text: "public answer"},
		PassAuthority: &interaction.PlanAuthority{
			ReadAuthorities: []readsource.Authority{},
			Reads:           []interaction.PassContextDependency{},
		},
	}
}

func TestSavedPlanRequiresExplicitProvenance(t *testing.T) {
	t.Parallel()
	valid := publicPlan()
	require.NoError(t, valid.Validate()) // Generation zero and explicit empty evidence are valid.
	for _, mutate := range []struct {
		name  string
		apply func(*interaction.SavedPlan)
	}{
		{"missing_kind", func(p *interaction.SavedPlan) { p.Kind = "" }},
		{"missing_state", func(p *interaction.SavedPlan) { p.State = "" }},
		{"missing_authority", func(p *interaction.SavedPlan) { p.PassAuthority = nil }},
		{"missing_leaves", func(p *interaction.SavedPlan) { p.PassAuthority.ReadAuthorities = nil }},
		{"missing_context", func(p *interaction.SavedPlan) { p.PassAuthority.Reads = nil }},
		{"negative_generation", func(p *interaction.SavedPlan) { p.HistoryGeneration = -1 }},
		{"derived_command", func(p *interaction.SavedPlan) { p.OrderCommand = &orders.Command{} }},
		{"command_without_command", func(p *interaction.SavedPlan) { p.Kind = interaction.CommandPlan }},
		{"notice_without_catalog", func(p *interaction.SavedPlan) { p.Kind = interaction.NoticePlan }},
		{"ambiguous_commands", func(p *interaction.SavedPlan) {
			p.Kind = interaction.CommandPlan
			p.OrderCommand = &orders.Command{}
			p.RegistrationCommand = &passbooking.Command{}
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			t.Parallel()
			plan := publicPlan()
			mutate.apply(&plan)
			require.Error(t, plan.Validate())
		})
	}
}

func TestSavedTerminalCannotCarryModelText(t *testing.T) {
	t.Parallel()
	plan := interaction.SavedPlan{FormatVersion: interaction.CurrentFormatVersion,
		Kind:           interaction.TerminalPlan,
		State:          interaction.PrivacyTerminal,
		TerminalReason: interaction.SourceRevoked,
	}
	require.NoError(t, plan.Validate())
	plan.Plan.Text = "private answer"
	require.Error(t, plan.Validate())
	plan = interaction.SavedPlan{FormatVersion: interaction.CurrentFormatVersion,
		Kind:         interaction.NoticePlan,
		State:        interaction.Ready,
		SystemNotice: i18n.AgentSourceUnavailable,
	}
	require.NoError(t, plan.Validate())
}

func TestSavedPlanFormatVersion(t *testing.T) {
	t.Parallel()
	for _, version := range []int{0, -1, 2} {
		plan := publicPlan()
		plan.FormatVersion = version
		require.ErrorIs(t, plan.Validate(), interaction.ErrUnsupportedFormat)
	}
	plan := publicPlan()
	plan.FormatVersion = 0
	plan.BindKind()
	require.Equal(t, interaction.CurrentFormatVersion, plan.FormatVersion)
	require.NoError(t, plan.Validate())
}

func TestSavedOrderRequiresMatchingHistoryGeneration(t *testing.T) {
	t.Parallel()
	plan := publicPlan()
	plan.Kind = interaction.CommandPlan
	plan.OrderCommand = &orders.Command{Name: "create", Origin: "agent"}
	require.Error(t, plan.Validate())
	zero := int64(0)
	plan.OrderCommand.HistoryGeneration = &zero
	require.NoError(t, plan.Validate())
	plan.HistoryGeneration = 1
	require.Error(t, plan.Validate())
	plan.OrderCommand.Origin = "manual"
	require.NoError(t, plan.Validate())
}
