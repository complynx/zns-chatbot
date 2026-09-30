package interaction_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// Both ingress paths must retain independent actor, booking, invitation and
// payment versions. These intentionally differ to catch identity substitution.
func TestRegistrationTargetAdmissionParity(t *testing.T) {
	t.Parallel()
	actor := passbooking.Booking{Event: "event", Owner: "actor", Version: 11}
	target := passbooking.Booking{Event: "event", Owner: "target", Version: 23}
	invitation := passbooking.Invitation{From: passbooking.Contact{Owner: "target"}, Version: 37}
	payment := passbooking.Payment{Event: "event", Version: 41, Attempt: "proof-attempt", Decision: "pending"}
	takeover := passbooking.TakeoverTarget{Booking: target, ActorVersion: 53}
	for _, name := range []string{"accept", "decline", "admin_cancel", "admin_uncouple", "proof_accept", "proof_reject",
		passbooking.CommandTakeover, passbooking.CommandReceivedOnly} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			proposal := agent.RegistrationProposal{Name: name, Event: "event", Target: "target"}
			read := agent.RegistrationReadResult{Request: agent.RegistrationProposal{Event: "event"}, Booking: &actor}
			expected := passbooking.Command{
				Name:          name,
				Event:         "event",
				Version:       11,
				Target:        "target",
				TargetVersion: 23,
			}
			var manual passbooking.Command
			switch name {
			case "accept", "decline":
				read.Request.View, read.Invitations = "invitations", []passbooking.Invitation{invitation}
				manual = interaction.RegistrationInvitationCommand(name, "event", actor.Version, invitation)
				expected.TargetVersion = 37
			case "admin_cancel", "admin_uncouple":
				read.Request.View, read.Queue = "queue", []passbooking.Booking{target}
				manual = interaction.RegistrationQueueCommand(name, "event", actor.Version, target)
			case "proof_accept", "proof_reject":
				read.Request.View = "payment_queue"
				read.PaymentQueue = []passbooking.PaymentReview{{Owner: "target", Payment: payment}}
				manual = interaction.RegistrationPaymentReviewCommand(name, "event", actor.Version, "target", payment)
				expected.TargetVersion, expected.PaymentAttempt = 41, "proof-attempt"
			default:
				read.Request.View, read.TakeoverTarget = agent.RegistrationTakeoverTarget, &takeover
				manual = interaction.RegistrationTakeoverCommand(name, "event", takeover)
				expected.Version = 53
			}
			input := agent.Input{Registration: &agent.RegistrationContext{
				Events: []passbooking.Event{{ID: "event"}}, Reads: []agent.RegistrationReadResult{read},
			}}
			bound, _, err := interaction.BindRegistrationPlan("", agent.Plan{RegistrationAction: &proposal}, input)
			require.NoError(t, err)
			require.Equal(t, expected, manual)
			require.Equal(t, manual, *bound)
		})
	}
}

func TestRegistrationAssignmentDraftContinuity(t *testing.T) {
	t.Parallel()
	target := passbooking.AdminTarget{Booking: passbooking.Booking{Event: "new-event", Owner: "target", Version: 23},
		ActorVersion: 11, ProfileVersion: 37}
	price := 100
	previous := passbooking.AdminAssignment{Event: "new-event", Key: "existing-key", Version: 11,
		Target: "target", TargetVersion: 23, TotalPrice: &price,
		Create: &passbooking.AdminCreate{FromProfile: true, ProfileVersion: 37}}
	require.Equal(t, previous, interaction.RegistrationAssignmentDraft("new-event", target, &previous))
	for _, changed := range []string{"event", "actor", "target", "booking", "profile"} {
		t.Run(changed, func(t *testing.T) {
			t.Parallel()
			stale := previous
			switch changed {
			case "event":
				stale.Event = "old-event"
			case "actor":
				stale.Version++
			case "target":
				stale.Target = "another-owner"
			case "booking":
				stale.TargetVersion++
			case "profile":
				stale.Create = &passbooking.AdminCreate{FromProfile: true, ProfileVersion: 38}
			}
			require.Equal(t, passbooking.AdminAssignment{
				Event:         "new-event",
				Version:       11,
				Target:        "target",
				TargetVersion: 23,
			}, interaction.RegistrationAssignmentDraft("new-event", target, &stale))
		})
	}
}

func TestRegistrationAssignmentAgentUsesObservedIdentity(t *testing.T) {
	t.Parallel()
	target := passbooking.AdminTarget{Booking: passbooking.Booking{Event: "event", Owner: "target", Version: 23},
		ActorVersion: 11, ProfileVersion: 37, CanAssign: true}
	input := agent.Input{Registration: &agent.RegistrationContext{Reads: []agent.RegistrationReadResult{{
		Request: agent.RegistrationProposal{Event: "event", View: agent.RegistrationAdminTarget}, AdminTarget: &target,
	}}}}
	proposal := agent.RegistrationProposal{Name: agent.RegistrationAdminAssign, Event: "event", Target: "target",
		Assignment: &agent.RegistrationAssignment{}}
	command, _, err := interaction.BindAdminAssignment("", agent.Plan{RegistrationAction: &proposal}, input)
	require.NoError(t, err)
	require.Equal(t, interaction.RegistrationAssignmentDraft("event", target, nil), *command)
}
