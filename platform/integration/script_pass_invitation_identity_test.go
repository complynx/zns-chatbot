package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestScriptPassInvitationReplacement(t *testing.T) {
	t.Parallel()
	for _, code := range []string{`return tools.passes.invitations({event:"dance"});`, `return tools.passes.registration.read({event:"dance",view:"invitations"});`} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET name='withdrawn-inviter-canary' WHERE id='alice'`)
			require.NoError(t, err)
			service := passbooking.Service{DB: f.db}
			command := bookingCommand("invite", "independent-invite-first", passbooking.Booking{})
			command.InviteTelegramID = 202
			first, err := service.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			update := pausePrivatePassTool(t, f, code, "withdrawn-inviter-canary")
			_, err = service.Execute(t.Context(), "alice", bookingCommand("cancel", "independent-withdraw", first))
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`DELETE FROM core.pass_registration_announcements WHERE event_id='dance' AND owner='alice'; DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'; UPDATE core.users SET name='current-inviter-name' WHERE id='alice'`,
			)
			require.NoError(t, err)
			command.Key = "independent-invite-second"
			second, err := service.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			require.Equal(t, first.Version, second.Version)
			require.NotEqual(t, first.CreatedAt, second.CreatedAt)
			current, err := service.Invitations(t.Context(), "bob", "dance", "")
			require.NoError(t, err)
			require.Len(t, current.Invitations, 1)
			require.Equal(t, "current-inviter-name", current.Invitations[0].From.Name)
			t.Logf(
				"replaced identity: version %d -> %d, creation %s -> %s",
				first.Version,
				second.Version,
				first.CreatedAt,
				second.CreatedAt,
			)
			input := retryPassDiscovery(t, f, update)
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "withdrawn-inviter-canary")
		})
	}
}
