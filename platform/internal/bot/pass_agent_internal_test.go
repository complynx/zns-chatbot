package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestRegistrationContactsAndGrounding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text string
		message    telegram.Message
		allowed    bool
	}{
		{name: "explicit", text: "Invite Telegram ID 202", allowed: true},
		{name: "substring", text: "Invite 2020"},
		{name: "username", text: "Invite @202"},
		{name: "negative_chat", text: "Invite -202"},
		{name: "contact", message: telegram.Message{Contact: &telegram.Contact{UserID: 202}}, allowed: true},
		{name: "forward", message: telegram.Message{ForwardOrigin: &telegram.ForwardOrigin{Type: "user", SenderUser: &telegram.User{ID: 202}}}, allowed: true},
		{name: "hidden", message: telegram.Message{ForwardOrigin: &telegram.ForwardOrigin{Type: "hidden_user", SenderUser: &telegram.User{ID: 202}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := agent.Input{
				Text:         tc.text,
				Registration: &agent.RegistrationContext{TrustedPartnerIDs: registrationContacts(&tc.message)},
			}
			assert.Equal(t, tc.allowed, groundedRegistrationContact(input, 202))
		})
	}
}

func TestRegistrationContextBudgetCannotLeakOversizedResults(t *testing.T) {
	t.Parallel()
	value := &agent.RegistrationContext{
		Reads: []agent.RegistrationReadResult{
			{Events: []passbooking.Event{{ID: strings.Repeat("x", registrationContextBytes)}}},
		},
	}
	require.NoError(t, boundRegistrationContext(value))
	assert.True(t, value.Reads[0].Omitted)
	assert.Empty(t, value.Reads[0].Events)
	value.Events = []passbooking.Event{{ID: strings.Repeat("x", registrationContextBytes)}}
	require.Error(t, boundRegistrationContext(value))
}

func TestRegistrationBindingRequiresExactAuthorizedEvidence(t *testing.T) {
	t.Parallel()
	context := &agent.RegistrationContext{
		Events: []passbooking.Event{{ID: "dance"}},
		Reads: []agent.RegistrationReadResult{
			{
				Request: agent.RegistrationProposal{Event: "dance", View: "home"},
				Booking: &passbooking.Booking{Event: "dance", Version: 7},
			},
		},
	}
	input := agent.Input{Text: "invite 202", Registration: context}
	plan := agent.Plan{
		View:               agent.RegistrationView,
		RegistrationAction: &agent.RegistrationProposal{Name: passInvite, Event: "dance", InviteTelegramID: 202},
	}
	command, _, err := bindRegistrationPlan(plan, input)
	require.NoError(t, err)
	assert.EqualValues(t, 7, command.Version)
	plan.RegistrationAction.Name = "admin_cancel"
	plan.RegistrationAction.InviteTelegramID = 0
	plan.RegistrationAction.Target = "bob"
	_, _, err = bindRegistrationPlan(plan, input)
	require.Error(t, err)
	context.Reads = append(
		context.Reads,
		agent.RegistrationReadResult{
			Request: agent.RegistrationProposal{Event: "dance", View: passMenuQueue},
			Queue:   []passbooking.Booking{{Owner: "bob", Version: 9}},
		},
	)
	command, _, err = bindRegistrationPlan(plan, input)
	require.NoError(t, err)
	assert.EqualValues(t, 9, command.TargetVersion)
	context.Reads[1].Error = mediaForbidden
	_, _, err = bindRegistrationPlan(plan, input)
	require.Error(t, err)
}
