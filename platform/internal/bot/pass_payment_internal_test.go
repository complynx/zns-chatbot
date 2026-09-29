package bot

import (
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRegistrationReceiptGroundingAndCrossDomainAmounts(t *testing.T) {
	t.Parallel()
	candidates := []agent.MediaCandidate{
		{OrderID: "order", Version: 2, AmountRUB: "100.00"},
		{
			RegistrationEvent:  "dance",
			RegistrationTitles: map[string]string{"ru": "Танцы"},
			Version:            7,
			AmountRUB:          "100",
		},
	}
	p := agent.MediaProposal{Amount: "100", Currency: "RUB"}
	assert.Nil(t, mediaMatch(p, candidates, "", ""), "same sum across domains needs a choice")
	p.RegistrationEvent = "dance"
	assert.Nil(t, mediaMatch(p, candidates, "", "unrelated"), "model reference alone is not a user choice")
	match := mediaMatch(p, candidates, "", "Вот чек за ТАНЦЫ")
	require.NotNil(t, match)
	assert.EqualValues(t, 7, match.Version)
	assert.Equal(t, "dance", match.RegistrationEvent)
	assert.Empty(t, match.OrderID)
}

func TestRegistrationReviewBindsAuthorizedAttempt(t *testing.T) {
	t.Parallel()
	p := agent.RegistrationProposal{Name: registrationProofAccept, Event: "dance", Target: "alice"}
	input := agent.Input{Registration: &agent.RegistrationContext{
		Events: []passbooking.Event{{ID: "dance"}},
		Reads: []agent.RegistrationReadResult{
			{Request: agent.RegistrationProposal{Event: "dance", View: registrationPaymentQueue},
				Booking: &passbooking.Booking{Event: "dance", Owner: "bob", Version: 3},
				PaymentQueue: []passbooking.PaymentReview{{Owner: "alice", Payment: passbooking.Payment{
					Attempt: "immutable-attempt", Decision: "pending", Version: 8,
				}}},
			},
		},
	}}
	command, _, err := interaction.BindRegistrationPlan(
		agenthost.CurrentRequestEvidence(input),
		agent.Plan{View: agent.RegistrationView, RegistrationAction: &p},
		input,
	)
	require.NoError(t, err)
	assert.EqualValues(t, 3, command.Version)
	assert.EqualValues(t, 8, command.TargetVersion)
	assert.Equal(t, "immutable-attempt", command.PaymentAttempt)
	input.Registration.Reads[0].Error = mediaForbidden
	_, _, err = interaction.BindRegistrationPlan(
		agenthost.CurrentRequestEvidence(input),
		agent.Plan{View: agent.RegistrationView, RegistrationAction: &p},
		input,
	)
	require.Error(t, err)
}
