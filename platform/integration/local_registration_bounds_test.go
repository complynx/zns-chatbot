package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func registrationBoundsClients(t *testing.T, s passbooking.Service) []appclient.Client {
	t.Helper()
	signer := identity.Signer{Key: []byte("local-registration-bounds-test-key")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.Services{Core: core.Service{DB: s.DB}, Registration: s},
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	return []appclient.Client{
		{
			SandboxToken: signer.Token,
			LocalRegistration: &appclient.LocalRegistration{
				Service:    s,
				Authorizer: applicationauth.Authorizer{DB: s.DB, Verify: verify},
			},
		},
		{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token},
	}
}

func TestRegistrationCommandBoundsHTTP(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		set  func(*passbooking.Command)
	}{
		{"target", func(c *passbooking.Command) { c.Target = strings.Repeat("x", 201) }},
		{"payment admin", func(c *passbooking.Command) { c.PaymentAdmin = strings.Repeat("x", 201) }},
		{"proof", func(c *passbooking.Command) { c.ProofID = strings.Repeat("x", 201) }},
		{"payment attempt", func(c *passbooking.Command) { c.PaymentAttempt = strings.Repeat("x", 201) }},
		{"multi megabyte target", func(c *passbooking.Command) { c.Target = strings.Repeat("x", 2<<20) }},
		{"nul target", func(c *passbooking.Command) { c.Target = "alice\x00extra" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db, s := bookingFixture(t)
			command := bookingCommand("solo", "bounded-command", passbooking.Booking{})
			test.set(&command)
			for _, client := range registrationBoundsClients(t, s) {
				result, err := client.ExecutePassBooking(t.Context(), "alice", command)
				require.Error(t, err)
				assert.Zero(t, result.Version)
			}
			var bookings, receipts int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.pass_bookings),(SELECT count(*) FROM core.pass_booking_operations)`).
					Scan(&bookings, &receipts),
			)
			assert.Zero(t, bookings, "rejected command must not create a booking")
			assert.Zero(t, receipts, "rejected command must not create an operation receipt")
		})
	}
}

func TestRegistrationCatalogBoundsHTTP(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, query, text string }{
		{"title", `UPDATE core.pass_events SET titles=jsonb_build_object('en',$1::text) WHERE id='dance'`, strings.Repeat("x", core.ReadResourceBytes+1)},
		{"short title", `UPDATE core.pass_events SET short_titles=jsonb_build_object('ru',$1::text) WHERE id='dance'`, strings.Repeat("x", core.ReadResourceBytes+1)},
		{"country", `UPDATE core.pass_events SET country_emoji=$1 WHERE id='dance'`, strings.Repeat("x", core.ReadResourceBytes+1)},
		{"aggregate", `INSERT INTO core.pass_events(id,finishes_at,titles) SELECT 'bounded-'||n,now()+interval '30 days',jsonb_build_object('en',$1::text) FROM generate_series(1,40) n`, strings.Repeat("x", 32<<10)},
		{"escaped aggregate", `UPDATE core.pass_events SET titles=jsonb_build_object('en',$1::text) WHERE id='dance'`, strings.Repeat("<", 200000)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db, s := bookingFixture(t)
			_, err := db.Exec(t.Context(), test.query, test.text)
			require.NoError(t, err)
			for _, client := range registrationBoundsClients(t, s) {
				events, readErr := client.PassEvents(t.Context(), "alice")
				if assert.Error(t, readErr) {
					var problem *core.ProblemError
					if assert.ErrorAs(t, readErr, &problem) {
						assert.Equal(t, "read_result_limit", problem.Code)
					}
				}
				assert.Empty(t, events, "oversized catalogs must not return partial events")
			}
		})
	}
}

func TestRegistrationCatalogKeepsCountAndOrdering(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET titles='{"en":"English","ru":"Русский"}',short_titles='{"en":"E","ru":"Р"}',country_emoji='🇳🇱' WHERE id='dance'; INSERT INTO core.pass_events(id,finishes_at,display_order) VALUES('later-a',now()+interval '30 days',2),('later-b',now()+interval '30 days',1)`,
	)
	require.NoError(t, err)
	clients := registrationBoundsClients(t, s)
	for _, client := range clients {
		events, readErr := client.PassEvents(t.Context(), "alice")
		require.NoError(t, readErr)
		require.Len(t, events, 3)
		require.Equal(t, []string{"dance", "later-b", "later-a"}, []string{events[0].ID, events[1].ID, events[2].ID})
		require.Equal(t, map[string]string{"en": "English", "ru": "Русский"}, events[0].Titles)
		require.Equal(t, "🇳🇱", events[0].CountryEmoji)
	}
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) SELECT 'count-'||n,now()+interval '30 days' FROM generate_series(1,98) n; UPDATE core.pass_events SET titles=jsonb_build_object('en',repeat('x',1100000)) WHERE id='dance'`,
	)
	require.NoError(t, err)
	for _, client := range clients {
		events, readErr := client.PassEvents(t.Context(), "alice")
		requireCode(t, readErr, "pass_event_limit")
		require.Empty(t, events)
	}
}
