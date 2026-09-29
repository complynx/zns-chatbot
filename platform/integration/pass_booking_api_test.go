package integration_test

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassBookingAPICatalogInvitationAndActor(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET titles='{"en":"Dance","ru":"Танцы"}' WHERE id='dance';
 INSERT INTO core.pass_events(id,finishes_at) VALUES('past',clock_timestamp()-interval '1 hour');
 INSERT INTO core.pass_payment_admins(event_id,owner,hidden) VALUES('dance','visitor',true)`)
	require.NoError(t, err)
	signer := identity.Signer{Key: []byte(strings.Repeat("p", 32))}
	server := httptest.NewServer(
		api.Handler(appservices.NewServices(db, appservices.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	defer server.Close()
	client := appclient.Client{Base: server.URL, SandboxToken: signer.Token, HTTP: server.Client()}
	events, err := client.PassEvents(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "Танцы", events[0].Titles["ru"])
	assert.NotNil(t, events[0].SalesStart)
	admins, err := client.PassPaymentAdmins(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Len(t, admins, 1)
	assert.Equal(t, "bob", admins[0].Owner)
	command := bookingCommand("invite", "api-invite", passbooking.Booking{})
	command.InviteTelegramID = 202
	inviter, err := client.ExecutePassBooking(t.Context(), "alice", command)
	require.NoError(t, err)
	page, err := client.PassInvitations(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	invitations := page.Invitations
	require.Len(t, invitations, 1)
	assert.Equal(t, "alice", invitations[0].From.Owner)
	assert.Equal(t, inviter.Version, invitations[0].Version)
	page, err = client.PassInvitations(t.Context(), "visitor", "dance", "")
	require.NoError(t, err)
	assert.Empty(t, page.Invitations)
	accept := bookingCommand("accept", "api-accept", passbooking.Booking{})
	accept.Target = "alice"
	accept.TargetVersion = inviter.Version
	_, err = client.ExecutePassBooking(t.Context(), "visitor", accept)
	requireCode(t, err, "forbidden")
	accepted, err := client.ExecutePassBooking(t.Context(), "bob", accept)
	require.NoError(t, err)
	assert.Equal(t, "assigned", accepted.State)
	replay, err := client.ExecutePassBooking(t.Context(), "bob", accept)
	require.NoError(t, err)
	assert.Equal(t, accepted, replay)
	_, err = client.PassEvents(t.Context(), "unknown")
	requireCode(t, err, "forbidden")
}
