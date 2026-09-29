package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestProviderCapabilitiesHideUnauthorizedDiscovery(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"ordinary", "payment", "global", "reviewer", "curator", "revoked"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			input := capabilityTestInput(role)
			checks := 0
			input.BeforeProvider = func(_ context.Context, current *Input) error {
				checks++
				if role == "revoked" && checks == 2 {
					current.Registration.Capabilities = nil
				}
				return nil
			}
			calls := 0
			_, err := planWithSkills(
				t.Context(),
				input,
				func(_ context.Context, prompt providerPrompt) (string, error) {
					calls++
					checkCapabilityPrompt(t, role, calls, prompt)
					if calls == 1 {
						return `{"skills":["registration","knowledge"],"reply_language":"en"}`, nil
					}
					if role == "payment" {
						assert.Contains(t, prompt.schema, "proof_accept")
						assert.Contains(t, prompt.instructions, "proof_accept")
					}
					if role == "reviewer" {
						assert.Contains(t, prompt.schema, knowledgeReviewCard)
						assert.Contains(t, prompt.instructions, knowledgeReviewCard)
					}
					return emptyActionsPlan, nil
				},
			)
			require.NoError(t, err)
			assert.Equal(t, 2, calls)
			assert.Equal(t, 2, checks)
		})
	}
}

func TestGuessedCapabilityAndCrossEventProposalsAreRejected(t *testing.T) {
	t.Parallel()
	input := Input{
		Registration: &RegistrationContext{
			Capabilities: []passbooking.Capabilities{{Event: "a", Actions: []string{"proof_accept"}}},
		},
		Knowledge: &KnowledgeContext{Scopes: []knowledge.Scope{{Event: "a", CanReview: true}}},
	}
	for _, event := range []string{"a", "b"} {
		plan := Plan{RegistrationAction: &RegistrationProposal{Name: "proof_accept", Event: event}}
		if event == "a" {
			require.NoError(t, validateVisiblePlan(input, plan))
		} else {
			require.Error(t, validateVisiblePlan(input, plan))
		}
	}
	require.Error(
		t,
		validateVisiblePlan(
			input,
			Plan{RegistrationAction: &RegistrationProposal{Name: RegistrationAdminAssign, Event: "a"}},
		),
	)
	require.Error(
		t,
		validateVisiblePlan(input, Plan{KnowledgeAction: &KnowledgeProposal{Name: knowledgeReviewCard, Event: "b"}}),
	)
	require.Error(
		t,
		validateVisiblePlan(
			input,
			Plan{KnowledgeAction: &KnowledgeProposal{Name: KnowledgeProposals, Event: "b", ReviewQueue: true}},
		),
	)
	input.Registration.Reads = []RegistrationReadResult{
		{Request: RegistrationProposal{Name: RegistrationRead, Event: "b", View: "payment_queue"}},
	}
	input.Knowledge.Reads = []KnowledgeReadResult{
		{Request: KnowledgeProposal{Name: KnowledgeProposals, Event: "b", ReviewQueue: true}},
	}
	visible := visibleProviderInput(input)
	assert.Empty(t, visible.Registration.Reads)
	assert.Empty(t, visible.Knowledge.Reads)
	assert.Len(t, input.Registration.Reads, 1)
	assert.Len(t, input.Knowledge.Reads, 1)
}

func TestProjectedSchemaRemainsStrictAndOrdinaryReadsAvailable(t *testing.T) {
	t.Parallel()
	schema, err := visiblePlanSchema(Input{})
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(schema), &value))
	assert.Equal(t, false, value["additionalProperties"])
	assert.Contains(t, schema, `"read"`)
	assert.NotContains(t, schema, `"total_price"`)
	assert.True(t, CanRegistrationView(Input{}, "new-event", "home"))
	assert.False(t, CanRegistrationView(Input{}, "new-event", "payment_queue"))
}

func capabilityTestInput(role string) Input {
	input := Input{Registration: &RegistrationContext{}, Knowledge: &KnowledgeContext{}}
	if role == "payment" || role == "revoked" {
		input.Registration.Capabilities = []passbooking.Capabilities{
			{
				Event:   "a",
				Actions: []string{"proof_accept", "proof_reject", "takeover", "received_only", "admin_cancel"},
			},
		}
	}
	if role == "global" {
		input.Registration.Capabilities = []passbooking.Capabilities{
			{
				Event: "a",
				Actions: []string{
					RegistrationAdminAssign,
					"admin_cancel",
					"admin_uncouple",
					"recalculate",
					"takeover",
					"received_only",
				},
			},
		}
	}
	input.Knowledge.Scopes = []knowledge.Scope{
		{Event: "a", CanReview: role == "reviewer", CanCurate: role == "curator"},
	}
	return input
}

func checkCapabilityPrompt(t *testing.T, role string, calls int, prompt providerPrompt) {
	t.Helper()
	all := prompt.instructions + prompt.schema + string(prompt.input)
	payment := role == "payment" || role == "revoked" && calls == 1
	if !payment {
		assert.NotContains(t, all, "proof_accept")
		assert.NotContains(t, all, "proof_reject")
		assert.NotContains(t, all, "payment_queue")
	}
	if role != "global" {
		assert.NotContains(t, all, RegistrationAdminAssign)
		assert.NotContains(t, all, "admin_uncouple")
		assert.NotContains(t, all, "append_tier")
	}
	if role != "reviewer" {
		assert.NotContains(t, all, knowledgeReviewCard)
	}
	if role != "curator" {
		assert.NotContains(t, all, "remove_fact")
	}
}
