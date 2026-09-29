package integration_test

import (
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func queuePaymentAuthorityFixture(t *testing.T) (conversation.Service, passbooking.ReadAuthority) {
	t.Helper()
	db, passes := bookingFixture(t)
	command := bookingCommand("solo", "queue-booking", passbooking.Booking{})
	command.PaymentAdmin = "bob"
	booking, err := passes.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("synthetic receipt"))
	require.NoError(t, err)
	command = bookingCommand("proof", "queue-proof", booking)
	command.ProofID = proof.ID
	booking, err = passes.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	queue, err := passes.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
	item := queue.Items[0]
	require.Equal(t, booking.CreatedAt, item.BookingCreatedAt)
	return conversation.Service{
		DB: db,
	}, passbooking.ReadAuthority{
		Kind:           passbooking.ReadPrivileged,
		Action:         "proof_accept",
		Event:          item.Payment.Event,
		Owner:          item.Owner,
		Version:        item.Payment.Version,
		CreatedAt:      item.BookingCreatedAt,
		PaymentAttempt: item.Payment.Attempt,
	}
}

func TestQueueAuthorityAdminIdentityPreservesEligibleHistory(t *testing.T) {
	t.Parallel()
	s, booking := conversationAuthorityFixture(t)
	_, err := s.DB.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	queue, err := (passbooking.Service{DB: s.DB}).Queue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Bookings, 1, "queue access does not require the target to retain can_book")
	authority := passbooking.ReadAuthority{
		Kind:      passbooking.ReadPrivileged,
		Action:    "admin_assign",
		Event:     booking.Event,
		Owner:     booking.Owner,
		Version:   booking.Version,
		CreatedAt: booking.CreatedAt,
	}
	require.NoError(t, s.AppendOriginal(t.Context(), "bob", "original", "assistant", "original correspondence"))
	require.NoError(
		t,
		s.AppendDerived(
			t.Context(),
			"bob",
			"queue-derived",
			"authorized queue detail",
			0,
			readsource.Registration([]passbooking.ReadAuthority{authority}),
		),
	)
	page, err := s.Read(t.Context(), "bob", conversation.Query{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, "authorized queue detail", page.Events[0].Text)
	_, err = s.DB.Exec(t.Context(), `DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'`)
	require.NoError(t, err)
	page, err = s.Read(t.Context(), "bob", conversation.Query{Limit: 2})
	require.NoError(t, err)
	assert.True(t, page.Events[0].Omitted)
	assert.Equal(t, "original correspondence", page.Events[1].Text)
	capabilities, err := (passbooking.Service{DB: s.DB}).Capabilities(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Contains(t, capabilities.Actions, "admin_assign", "the source row was revoked, not the actor grant")
}

func TestQueueAuthorityPaymentRequiresCurrentPendingParticipant(t *testing.T) {
	t.Parallel()
	mutations := map[string]string{
		"booking_deleted":      `DELETE FROM core.pass_registration_announcements WHERE owner='alice'; DELETE FROM core.pass_bookings WHERE owner='alice'`,
		"booking_reincarnated": `UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE owner='alice'`,
		"attempt_replaced": `INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at)
 SELECT repeat('f',64),event_id,submitter,proof_id,receiving_admin,received_at FROM core.pass_payment_attempts WHERE event_id='dance';
 UPDATE core.pass_bookings SET payment_attempt=repeat('f',64) WHERE owner='alice'`,
		"review_completed": `UPDATE core.pass_payment_attempts SET decision='accepted',reviewed_by='bob',reviewed_at=clock_timestamp() WHERE event_id='dance'`,
		"no_longer_paid":   `UPDATE core.pass_bookings SET state='assigned' WHERE owner='alice'`,
	}
	for name, query := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, authority := queuePaymentAuthorityFixture(t)
			require.NoError(t, s.AppendTrustedOutcome(t.Context(), "bob", "receipt", "committed receipt remains"))
			require.NoError(
				t,
				s.AppendDerived(
					t.Context(),
					"bob",
					"pending-review",
					"pending private review",
					0,
					readsource.Registration([]passbooking.ReadAuthority{authority}),
				),
			)
			require.NoError(
				t,
				s.CheckReadAuthorities(
					t.Context(),
					"bob",
					readsource.Registration([]passbooking.ReadAuthority{authority}),
				),
			)
			_, err := s.DB.Exec(t.Context(), query)
			require.NoError(t, err)
			requireCode(
				t,
				s.CheckReadAuthorities(
					t.Context(),
					"bob",
					readsource.Registration([]passbooking.ReadAuthority{authority}),
				),
				"history_stale",
			)
			page, err := s.Read(t.Context(), "bob", conversation.Query{Limit: 2})
			require.NoError(t, err)
			require.Len(t, page.Events, 2)
			assert.True(t, page.Events[0].Omitted)
			assert.Equal(t, "committed receipt remains", page.Events[1].Text)
			assert.EqualValues(t, 1, page.Generation)
		})
	}
}
