package integration_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassPaymentCoupleReviewAndReplacement(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	accept := bookingCommand("accept", "accept", passbooking.Booking{})
	accept.Target, accept.TargetVersion = "alice", alice.Version
	_, err = service.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET price=101 WHERE event_id='dance' AND owner='bob'`)
	require.NoError(t, err)
	quote, err := service.PaymentQuote(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.EqualValues(t, 201, quote.Total, "pair can have unequal participant prices")
	assert.Equal(t, "RUB", quote.Currency)
	assert.Equal(t, alice.Version, quote.Version)
	proof, err := (orders.Service{DB: db}).UploadProof(
		t.Context(),
		"alice",
		"receipt.txt",
		[]byte("synthetic paid 200"),
	)
	require.NoError(t, err)
	submit := bookingCommand("proof", "proof1", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	assert.Equal(t, "paid", alice.State)
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "pending", payment.Decision)
	assert.Equal(t, "bob", payment.ReceivingAdmin)
	queue, err := service.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1, "a shared couple receipt is one review task")
	assert.Equal(t, payment.Attempt, queue.Items[0].Payment.Attempt)
	_, err = service.PaymentQueue(t.Context(), "alice", "dance", "")
	requireCode(t, err, "forbidden")
	_, err = service.PaymentQueue(t.Context(), "bob", "dance", "bad cursor")
	requireCode(t, err, "pass_booking_invalid")
	peer, err := service.PaymentProof(t.Context(), "bob", "dance", "bob")
	require.NoError(t, err)
	assert.Equal(t, []byte("synthetic paid 200"), peer.Body)
	_, err = service.PaymentProof(t.Context(), "visitor", "dance", "alice")
	requireCode(t, err, "forbidden")
	_, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err, "retry after commit must not create another attempt")
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	reject := bookingCommand("proof_reject", "reject", bob)
	reject.Target, reject.TargetVersion, reject.PaymentAttempt = "alice", alice.Version, payment.Attempt
	_, err = service.Execute(t.Context(), "bob", reject)
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", alice.State)
	bob, err = service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", bob.State)
	submit = bookingCommand("proof", "proof2", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	next, err := service.Payment(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	assert.NotEqual(t, payment.Attempt, next.Attempt)
	bob, err = service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	stale := bookingCommand("proof_accept", "old-attempt", bob)
	stale.Target, stale.TargetVersion, stale.PaymentAttempt = "alice", alice.Version, payment.Attempt
	_, err = service.Execute(t.Context(), "bob", stale)
	requireCode(t, err, "pass_payment_stale")
	stale.Key, stale.PaymentAttempt = "new-attempt", next.Attempt
	_, err = service.Execute(t.Context(), "bob", stale)
	require.NoError(t, err)
	accepted, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "accepted", accepted.Decision)
	require.NotNil(t, accepted.ReviewedBy)
	assert.Equal(t, "bob", *accepted.ReviewedBy)
	queue, err = service.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	assert.Empty(t, queue.Items)
	var history int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&history))
	assert.Equal(t, 2, history)
}

func TestPassPaymentAuthorizationAndConcurrentDecisions(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "solo", passbooking.Booking{}))
	require.NoError(t, err)
	foreign, err := (orders.Service{DB: db}).UploadProof(t.Context(), "bob", "other.txt", []byte("not Alice's file"))
	require.NoError(t, err)
	submit := bookingCommand("proof", "submit", alice)
	submit.ProofID = foreign.ID
	_, err = service.Execute(t.Context(), "alice", submit)
	requireCode(t, err, "forbidden")
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "own.txt", []byte("synthetic proof"))
	require.NoError(t, err)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('visitor')`)
	require.NoError(t, err)
	review := bookingCommand("proof_accept", "review", passbooking.Booking{})
	review.Target, review.TargetVersion, review.PaymentAttempt = "alice", alice.Version, payment.Attempt
	_, err = service.Execute(t.Context(), "visitor", review)
	requireCode(t, err, "forbidden")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_payment_admins SET hidden=true WHERE event_id='dance' AND owner='bob'`,
	)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, action := range []string{"proof_accept", "proof_reject"} {
		wg.Go(func() {
			command := review
			command.Name, command.Key = action, action
			_, executeErr := service.Execute(t.Context(), "bob", command)
			errors <- executeErr
		})
	}
	wg.Wait()
	close(errors)
	success := 0
	for executeErr := range errors {
		if executeErr == nil {
			success++
		} else {
			requireCode(t, executeErr, "pass_payment_stale")
		}
	}
	assert.Equal(t, 1, success, "one atomic decision, including hidden payment admins")
}

func TestPassPaymentReviewNeverRevivesCancelledPartner(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	accept := bookingCommand("accept", "accept", passbooking.Booking{})
	accept.Target, accept.TargetVersion = "alice", alice.Version
	bob, err := service.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "bob", "receipt.txt", []byte("synthetic couple"))
	require.NoError(t, err)
	submit := bookingCommand("proof", "proof", bob)
	submit.ProofID = proof.ID
	bob, err = service.Execute(t.Context(), "bob", submit)
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	cancel := bookingCommand("admin_cancel", "cancel-partner", bob)
	cancel.Target, cancel.TargetVersion = "alice", alice.Version
	bob, err = service.Execute(t.Context(), "bob", cancel)
	require.NoError(t, err)
	assert.Equal(t, "paid", bob.State)
	assert.Empty(t, bob.Partner)
	payment, err := service.Payment(t.Context(), "bob", "dance", "bob")
	require.NoError(t, err)
	review := bookingCommand("proof_accept", "approve-survivor", bob)
	review.Target, review.TargetVersion, review.PaymentAttempt = "bob", bob.Version, payment.Attempt
	_, err = service.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	_, err = service.PaymentProof(t.Context(), "alice", "dance", "alice")
	requireCode(t, err, "forbidden")
	payment, err = service.Payment(t.Context(), "bob", "dance", "bob")
	require.NoError(t, err)
	assert.Equal(t, "accepted", payment.Decision)
}
