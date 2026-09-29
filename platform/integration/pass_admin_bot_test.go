package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassAdminTargetReadIsAuthorizedAndPrivate(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_profiles SET legal_name='PRIVATE LEGAL',passport='PRIVATE DOCUMENT' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = f.b.API.PassAdminTarget(t.Context(), "alice", "dance", 101)
	requireCode(t, err, "forbidden")
	target, err := f.b.API.PassAdminTarget(t.Context(), "bob", "dance", 101)
	require.NoError(t, err)
	assert.Equal(t, "alice", target.Booking.Owner)
	assert.True(t, target.HasLegalName)
	assert.True(t, target.CanCreateFromProfile)
	encoded, err := json.Marshal(target)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "PRIVATE")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = f.b.API.PassAdminTarget(t.Context(), "bob", "dance", 101)
	requireCode(t, err, "forbidden")
}

func TestPassAdminManualFreeAssignmentOwnerAndReplay(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='en' WHERE id='bob'`)
	require.NoError(t, err)
	handle(t, f.b, message(600, 202, "/passes"))
	handle(t, f.b, passMenuClick(t, f, 202, 601, "Dance"))
	handle(t, f.b, passMenuClick(t, f, 202, 602, "Registration queue"))
	handle(t, f.b, passMenuClick(t, f, 202, 603, "Assign / adjust pass · 101"))
	handle(t, f.b, passMenuClick(t, f, 202, 604, "Free pass"))
	f.model.plan = agent.Plan{View: "workflow", Text: "Four."}
	handle(t, f.b, message(607, 202, "What is two plus two?"))
	apply := passMenuClick(t, f, 202, 605, "Apply assignment")
	foreign := apply
	copyCallback := *apply.Callback
	copyCallback.From.ID = 101
	foreign.Callback, foreign.ID = &copyCallback, 606
	handle(t, f.b, foreign)
	service := passbooking.Service{DB: f.db}
	before, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", before.State)
	handle(t, f.b, apply)
	after, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "paid", after.State)
	require.NotNil(t, after.Price)
	assert.Zero(t, *after.Price)
	handle(t, f.b, apply)
	replayed, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, after.Version, replayed.Version)
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "free", payment.Kind)
}

func TestPassAdminAgentRejectsChangedEvidenceAndGuessedIdentity(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"profile_changed", "rights_revoked", "guessed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			calls := 0
			f.b.Model = avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
				calls++
				if calls == 1 {
					return agent.Plan{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
						Name:   agent.RegistrationRead,
						Event:  "dance",
						View:   agent.RegistrationAdminTarget,
						Target: "101",
					}}, nil
				}
				query := `UPDATE core.pass_profiles SET version=version+1 WHERE owner='alice'`
				if scenario == "rights_revoked" {
					query = `DELETE FROM core.pass_booking_admins WHERE owner='bob'`
				}
				_, err := f.db.Exec(ctx, query)
				require.NoError(t, err)
				return agent.Plan{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
					Name: agent.RegistrationAdminAssign, Event: "dance", Target: "alice",
					Assignment: &agent.RegistrationAssignment{Create: true, FromProfile: true}}}, nil
			})
			text := "Create a pass for 101 from their profile"
			if scenario == "guessed" {
				text = "Create a pass for some Alice"
			}
			handle(t, f.b, message(800, 202, text))
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings WHERE owner='alice'`).Scan(&count),
			)
			assert.Zero(t, count)
			if scenario == "guessed" {
				assert.Equal(t, 1, calls, "an invented numeric identity cannot trigger a private read")
			}
		})
	}
}

func TestPassAdminAgentCreatesExplicitIdentityAndBindsVersions(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	price, name, kind, comment := 125, "Alice Smith", "volunteer", "evening only"
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
			Name: agent.RegistrationRead, Event: "dance", View: agent.RegistrationAdminTarget, Target: "101"}},
		{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
			Name: agent.RegistrationAdminAssign, Event: "dance", Target: "alice",
			Assignment: &agent.RegistrationAssignment{TotalPrice: &price, Kind: &kind, Comment: &comment,
				Create: true, Role: "leader", LegalName: &name}}},
	}}
	f.b.Model = model
	handle(
		t,
		f.b,
		message(700, 202, "Создай для 101 заявку на Танцы: Alice Smith, лидер, volunteer, 125 рублей, evening only"),
	)
	require.Len(t, model.inputs, 2)
	require.NotNil(t, model.inputs[1].Registration.Reads[0].AdminTarget)
	booking, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", booking.State)
	require.NotNil(t, booking.Price)
	assert.Equal(t, price, *booking.Price)
	assert.Equal(t, kind, booking.Kind)
	assert.Equal(t, comment, booking.Comment)
	assert.Contains(t, passMenuCard(t, f, 202).Text, "Сохранено.")
	handle(
		t,
		f.b,
		message(700, 202, "Создай для 101 заявку на Танцы: Alice Smith, лидер, volunteer, 125 рублей, evening only"),
	)
	assert.Len(t, model.inputs, 2)
}
