package integration_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRegistrationSavedKeyPrecedesEffectAndReplay(t *testing.T) {
	t.Parallel()
	for _, assignment := range []bool{false, true} {
		t.Run(fmt.Sprintf("assignment=%v", assignment), func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			owner, user, route := "alice", int64(101), "/internal/derived/pass-actions"
			text := "Invite Telegram 202 to Dance"
			plans := []agent.Plan{registrationRead("home"), {
				View:               agent.RegistrationView,
				RegistrationAction: &agent.RegistrationProposal{Name: "invite", Event: "dance", InviteTelegramID: 202},
			}}
			const updateID = 68101
			wantKey := "tg-registration-68101"
			if assignment {
				owner, user, route = "bob", 202, "/internal/derived/pass-assignments"
				text = "Create pass for Telegram 101 using profile, total 150"
				wantKey = "tg-admin-assignment-68101"
				price := 150
				_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Synthetic Name'`)
				require.NoError(t, err)
				plans = []agent.Plan{
					{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
						Name:   agent.RegistrationRead,
						Event:  "dance",
						View:   agent.RegistrationAdminTarget,
						Target: "101",
					}},
					{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
						Name:   agent.RegistrationAdminAssign,
						Event:  "dance",
						Target: "alice",
						Assignment: &agent.RegistrationAssignment{
							Create:      true,
							FromProfile: true,
							TotalPrice:  &price,
						},
					}},
				}
			}
			model := &knowledgeModel{plans: plans}
			f.b.Model = model
			var observed interaction.SavedPlan
			var observationErr error
			requests := 0
			f.b.Host.HTTP = &http.Client{
				Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
					if request.URL.Path == route {
						requests++
						observed, observationErr = (interaction.Store{DB: f.db}).Load(
							request.Context(),
							owner,
							updateID,
						)
					}
					return http.DefaultTransport.RoundTrip(request)
				}),
			}
			update := message(updateID, user, text)
			handle(t, f.b, update)
			require.NoError(t, observationErr)
			require.Equal(t, 1, requests, "observe persisted binding before the domain effect")
			var witness passbooking.OperationWitness
			var err error
			if assignment {
				require.NotNil(t, observed.RegistrationAssignment)
				require.Equal(t, wantKey, observed.RegistrationAssignment.Key)
				witness, err = passbooking.AssignmentOperationWitness(owner, *observed.RegistrationAssignment)
			} else {
				require.NotNil(t, observed.RegistrationCommand)
				require.Equal(t, wantKey, observed.RegistrationCommand.Key)
				witness, err = passbooking.CommandOperationWitness(owner, *observed.RegistrationCommand)
			}
			require.NoError(t, err)
			var digest string
			keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(wantKey)))
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT request_hash FROM core.pass_booking_operations
WHERE actor=$1 AND event_id='dance' AND key_hash=$2`, owner, keyHash).Scan(&digest))
			require.Equal(t, witness.Digest, digest)
			calls := len(model.inputs)
			handle(t, f.b, update)
			require.Len(t, model.inputs, calls)
			var receipts int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations
WHERE actor=$1 AND event_id='dance' AND key_hash=$2`, owner, keyHash).Scan(&receipts))
			require.Equal(t, 1, receipts)
		})
	}
}
