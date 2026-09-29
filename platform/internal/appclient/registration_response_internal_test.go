package appclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type registrationResponseCase struct {
	name, good, bad string
	call            func(context.Context, Client, Host) (any, error)
}

func TestRegistrationHTTPResponseContract(t *testing.T) {
	t.Parallel()
	for _, test := range registrationResponseCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var payload atomic.Value
			payload.Store(test.good)
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, payload.Load().(string))
			}))
			t.Cleanup(server.Close)
			signer := identity.Signer{}
			client := Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
			host := Host{Base: server.URL, HTTP: server.Client(), UserToken: client.UserToken, Signer: signer}
			value, err := test.call(t.Context(), client, host)
			require.NoError(t, err)
			require.NotEmpty(t, value, "the fixture must produce a meaningful successful value")
			for _, body := range []string{test.bad, test.good + strings.Repeat(" ", MaxAPIBytes)} {
				payload.Store(body)
				before := requests.Load()
				value, err = test.call(t.Context(), client, host)
				require.Equal(t, before+1, requests.Load(), "must reach the HTTP decode boundary")
				require.Error(t, err)
				require.Empty(t, value)
			}
		})
	}
}
func registrationResponseCases() []registrationResponseCase {
	bookingGood, bookingBad := `{"owner":"alice"}`, `{"owner":"alice","version":"bad"}`
	profileGood, profileBad := `{"owner":"alice"}`, `{"owner":"alice","version":"bad"}`
	assignmentGood, assignmentBad := `{"assigned_count":1}`, `{"assigned_count":1,"bookings":123}`
	batchGood, batchBad := `[{"telegram_id":101}]`, `[{"telegram_id":101},123]`
	pageGood, pageBad := `{"next_cursor":"cursor"}`, `{"next_cursor":"cursor","items":123}`
	receiptGood, receiptBad := `{"found":true}`, `{"found":true,"result":123}`
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	return []registrationResponseCase{
		{
			"events",
			`[{"id":"dance"}]`,
			`[{"id":"dance"},123]`,
			func(ctx context.Context, c Client, _ Host) (any, error) { return c.PassEvents(ctx, "alice") },
		},
		{
			"booking",
			bookingGood,
			bookingBad,
			func(ctx context.Context, c Client, _ Host) (any, error) { return c.PassBooking(ctx, "alice", "dance") },
		},
		{"booking-command", bookingGood, bookingBad, func(ctx context.Context, c Client, _ Host) (any, error) {
			return c.ExecutePassBooking(ctx, "alice", passbooking.Command{})
		}},
		{
			"invitations",
			`{"next":"cursor"}`,
			`{"next":"cursor","invitations":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassInvitations(ctx, "alice", "dance", "")
			},
		},
		{
			"payment-admins",
			`[{"owner":"alice"}]`,
			`[{"owner":"alice"},123]`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassPaymentAdmins(ctx, "alice", "dance")
			},
		},
		{
			"queue",
			`{"next":"cursor"}`,
			`{"next":"cursor","bookings":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassQueue(ctx, "alice", "dance", "")
			},
		},
		{
			"profile",
			profileGood,
			profileBad,
			func(ctx context.Context, c Client, _ Host) (any, error) { return c.PassProfile(ctx, "alice") },
		},
		{
			"profile-command",
			profileGood,
			profileBad,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.ExecutePassProfile(ctx, "alice", passes.Command{})
			},
		},
		{
			"profile-history",
			`[{"version":1}]`,
			`[{"version":1},123]`,
			func(ctx context.Context, c Client, _ Host) (any, error) { return c.PassProfileHistory(ctx, "alice") },
		},
		{
			"profile-history-page",
			`{"next_before":1}`,
			`{"next_before":1,"items":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassProfileHistoryPage(ctx, "alice", 0)
			},
		},
		{
			"capabilities",
			`{"event":"dance"}`,
			`{"event":"dance","actions":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassCapabilities(ctx, "alice", "dance")
			},
		},
		{
			"admin-target",
			`{"name":"Alice"}`,
			`{"name":"Alice","booking":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassAdminTarget(ctx, "alice", "dance", 101)
			},
		},
		{"assignment", assignmentGood, assignmentBad, func(ctx context.Context, c Client, _ Host) (any, error) {
			return c.AssignPass(ctx, "alice", passbooking.AdminAssignment{})
		}},
		{
			"takeover",
			`{"name":"Alice"}`,
			`{"name":"Alice","booking":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassTakeoverTarget(ctx, "alice", "dance", 101)
			},
		},
		{
			"payment",
			`{"attempt":"attempt-1"}`,
			`{"attempt":"attempt-1","version":"bad"}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassPayment(ctx, "alice", "dance", "alice")
			},
		},
		{
			"payment-quote",
			`{"currency":"RUB"}`,
			`{"currency":"RUB","total":"bad"}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassPaymentQuote(ctx, "alice", "dance")
			},
		},
		{
			"payment-queue",
			`{"next":"cursor"}`,
			`{"next":"cursor","items":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassPaymentQueue(ctx, "alice", "dance", "")
			},
		},
		{
			"events-page",
			pageGood,
			pageBad,
			func(ctx context.Context, c Client, _ Host) (any, error) { return c.PassEventsPage(ctx, "alice", "") },
		},
		{
			"event-detail",
			`{"json":"{}"}`,
			`{"json":"{}","more":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassEventDetail(ctx, "alice", "dance", "")
			},
		},
		{
			"payment-history",
			pageGood,
			pageBad,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassPaymentHistory(ctx, "alice", "dance", "")
			},
		},
		{
			"owns-events",
			`true`,
			`123`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.OwnsPassEvents(ctx, "alice", []string{"dance"})
			},
		},
		{
			"tool-capabilities",
			`{"export":true}`,
			`{"export":true,"actions":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) { return c.PassToolCapabilities(ctx, "alice") },
		},
		{
			"tiers",
			`{"event":"dance"}`,
			`{"event":"dance","usage":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.PassTierStatus(ctx, "alice", "dance")
			},
		},
		{"manual-batch", batchGood, batchBad, func(ctx context.Context, c Client, _ Host) (any, error) {
			return c.RunPassBatch(ctx, "alice", passbooking.RuntimeBatch{})
		}},
		{
			"proof-upload",
			`{"id":"proof"}`,
			`{"id":"proof","filename":123}`,
			func(ctx context.Context, c Client, _ Host) (any, error) {
				return c.UploadPassProof(ctx, "alice", "proof.txt", []byte("synthetic"))
			},
		},
		{"derived-booking", bookingGood, bookingBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.ExecuteDerivedPassBooking(ctx, "alice", passbooking.Command{}, source)
		}},
		{"derived-assignment", assignmentGood, assignmentBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.AssignDerivedPass(ctx, "alice", passbooking.AdminAssignment{}, source)
		}},
		{"derived-profile", profileGood, profileBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.ExecuteDerivedPassProfile(ctx, "alice", passes.Command{}, source)
		}},
		{"derived-batch", batchGood, batchBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.RunDerivedPassBatch(ctx, "alice", passbooking.RuntimeBatch{}, source)
		}},
		{"booking-receipt", receiptGood, receiptBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.PassBookingReceipt(ctx, "alice", passbooking.Command{}, source)
		}},
		{"assignment-receipt", receiptGood, receiptBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.PassAssignmentReceipt(ctx, "alice", passbooking.AdminAssignment{}, source)
		}},
		{"profile-receipt", receiptGood, receiptBad, func(ctx context.Context, _ Client, h Host) (any, error) {
			return h.PassProfileReceipt(ctx, "alice", passes.Command{}, source)
		}},
		{
			"notifications",
			`[{"id":1}]`,
			`[{"id":1},123]`,
			func(ctx context.Context, _ Client, h Host) (any, error) { return h.PendingPassNotifications(ctx) },
		},
		{
			"announcement",
			`{"found":true,"announcement":{"id":1}}`,
			`{"found":true,"announcement":123}`,
			func(ctx context.Context, _ Client, h Host) (any, error) {
				value, found, err := h.ClaimRegistrationAnnouncement(ctx)
				return struct {
					Value passbooking.RegistrationAnnouncement
					Found bool
				}{value, found}, err
			},
		},
	}
}
