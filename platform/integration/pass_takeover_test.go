package integration_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassTakeoverCouplePermissionsVersionsAndReplay(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	accept := bookingCommand("accept", "accept", passbooking.Booking{})
	accept.Target, accept.TargetVersion = "alice", alice.Version
	_, err = s.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_booking_admins(owner) VALUES('alice'); UPDATE core.pass_events SET finishes_at=now()-interval '1 day'`,
	)
	require.NoError(t, err)
	target, err := s.TakeoverTarget(t.Context(), "alice", "dance", 202)
	require.NoError(t, err)
	c := passbooking.Command{
		Name:          passbooking.CommandTakeover,
		Event:         "dance",
		Key:           "takeover",
		Version:       target.ActorVersion,
		Target:        "bob",
		TargetVersion: target.Booking.Version,
	}
	_, err = s.Execute(t.Context(), "visitor", c)
	requireCode(t, err, "forbidden")
	_, err = s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	for _, owner := range []string{"alice", "bob"} {
		b, getErr := s.Get(t.Context(), owner, "dance")
		require.NoError(t, getErr)
		assert.Equal(t, "alice", b.PaymentAdmin)
		assert.Equal(t, "assigned", b.State)
	}
	var notices int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_notifications WHERE kind='payment_contact_changed'`).
			Scan(&notices),
	)
	assert.Equal(t, 2, notices)
	c.Key = "stale"
	_, err = s.Execute(t.Context(), "alice", c)
	requireCode(t, err, "pass_booking_stale")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	target, err = s.TakeoverTarget(t.Context(), "bob", "dance", 101)
	require.NoError(t, err, "event payment admin alone retains takeover rights")
	c = passbooking.Command{
		Name:          passbooking.CommandTakeover,
		Event:         "dance",
		Key:           "back",
		Version:       target.ActorVersion,
		Target:        "alice",
		TargetVersion: target.Booking.Version,
	}
	_, err = s.Execute(t.Context(), "bob", c)
	require.NoError(t, err)
}

func TestPassReceiverBackfillPaidOnlyAndAssignmentBound(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	alice, err := s.Execute(t.Context(), "alice", bookingCommand("solo", "solo", passbooking.Booking{}))
	require.NoError(t, err)
	c := passbooking.Command{
		Name:          passbooking.CommandReceivedOnly,
		Event:         "dance",
		Key:           "backfill",
		Target:        "alice",
		TargetVersion: alice.Version,
	}
	_, err = s.Execute(t.Context(), "bob", c)
	requireCode(t, err, "pass_payment_state")
	_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET state='paid' WHERE owner='alice'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", c)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", c)
	require.NoError(t, err)
	target, err := s.TakeoverTarget(t.Context(), "bob", "dance", 101)
	require.NoError(t, err)
	assert.Equal(t, "bob", target.ReceivingAdmin)
	assert.False(t, target.CanBackfill)
	assert.Equal(t, "bob", target.Booking.PaymentAdmin)
	c.Key, c.TargetVersion = "repeat-current", target.Booking.Version
	_, err = s.Execute(t.Context(), "bob", c)
	require.NoError(t, err)
	repeated, err := s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, target.Booking.Version, repeated.Version)
	var attempts, notices int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&attempts))
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_notifications WHERE kind='payment_contact_changed'`).
			Scan(&notices),
	)
	assert.Zero(t, attempts, "backfill does not manufacture a receipt")
	assert.Zero(t, notices)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET assigned_at=assigned_at+interval '1 second' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	target, err = s.TakeoverTarget(t.Context(), "bob", "dance", 101)
	require.NoError(t, err)
	assert.Empty(t, target.ReceivingAdmin, "old provenance cannot follow a new assignment")
	assert.True(t, target.CanBackfill)
}

func TestPassTakeoverPreservesActualReceiptReceiver(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	alice, err := s.Execute(t.Context(), "alice", bookingCommand("solo", "solo", passbooking.Booking{}))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(
		t.Context(),
		"alice",
		"receipt.txt",
		[]byte("actual synthetic receipt"),
	)
	require.NoError(t, err)
	submit := bookingCommand("proof", "proof", alice)
	submit.ProofID = proof.ID
	alice, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	before, err := s.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	c := passbooking.Command{
		Name:          passbooking.CommandTakeover,
		Event:         "dance",
		Key:           "takeover",
		Version:       alice.Version,
		Target:        "alice",
		TargetVersion: alice.Version,
	}
	alice, err = s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	c.Name, c.Key, c.Version, c.TargetVersion = passbooking.CommandReceivedOnly, "missing-only", alice.Version, alice.Version
	_, err = s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	after, err := s.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, before.Attempt, after.Attempt)
	assert.Equal(t, before.ProofID, after.ProofID)
	assert.Equal(t, before.ReceivingAdmin, after.ReceivingAdmin)
	assert.Equal(t, before.ReceivedAt, after.ReceivedAt)
	var backfills int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_receiver_backfills`).Scan(&backfills))
	assert.Zero(t, backfills)
}

func TestPassTakeoverGlobalContactReceiptDoesNotGrantReview(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	alice, err := s.Execute(t.Context(), "alice", bookingCommand("solo", "solo", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	alice, err = s.Execute(
		t.Context(),
		"alice",
		passbooking.Command{
			Name:          passbooking.CommandTakeover,
			Event:         "dance",
			Key:           "take",
			Version:       alice.Version,
			Target:        "alice",
			TargetVersion: alice.Version,
		},
	)
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(
		t.Context(),
		"alice",
		"receipt.txt",
		[]byte("new receipt after takeover"),
	)
	require.NoError(t, err)
	submit := bookingCommand("proof", "new-proof", alice)
	submit.ProofID = proof.ID
	alice, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	payment, err := s.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", payment.ReceivingAdmin)
	assert.Equal(t, "paid", alice.State)
	contacts, err := s.PaymentAdmins(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Contains(t, contacts, passbooking.Contact{Owner: "alice", Name: "Алиса", TelegramID: 101})
	_, err = s.PaymentQueue(t.Context(), "alice", "dance", "")
	requireCode(t, err, "forbidden")
	review := bookingCommand("proof_accept", "review", alice)
	review.Target, review.TargetVersion, review.PaymentAttempt = "alice", alice.Version, payment.Attempt
	_, err = s.Execute(t.Context(), "alice", review)
	requireCode(t, err, "forbidden")
	// The actual event payment administrator retains the review task.
	queue, err := s.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
}

func TestPassReceiverBackfillMixedCoupleAndExport(t *testing.T) {
	t.Parallel()
	for _, partnerState := range []string{"paid", "assigned"} {
		t.Run(partnerState, func(t *testing.T) {
			t.Parallel()
			db, s := adminPairFixture(t)
			_, err := db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET state='paid',assigned_at=now(),price=100; INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`,
			)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET state=$1 WHERE owner='bob'`, partnerState)
			require.NoError(t, err)
			// Alice has a real receipt, Bob has legacy state without an attempt.
			_, err = db.Exec(
				t.Context(),
				`INSERT INTO core.order_proofs(id,owner,filename,body) VALUES(repeat('a',64),'alice','test.txt','test'); INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at) VALUES(repeat('b',64),'dance','alice',repeat('a',64),'bob',now()); UPDATE core.pass_bookings SET payment_attempt=repeat('b',64) WHERE owner='alice'`,
			)
			require.NoError(t, err)
			target, err := s.TakeoverTarget(t.Context(), "alice", "dance", 101)
			require.NoError(t, err)
			assert.Equal(t, partnerState == "paid", target.CanBackfill)
			_, err = s.Execute(
				t.Context(),
				"alice",
				passbooking.Command{
					Name:          passbooking.CommandReceivedOnly,
					Event:         "dance",
					Key:           "mixed",
					Version:       target.ActorVersion,
					Target:        "alice",
					TargetVersion: target.Booking.Version,
				},
			)
			require.NoError(t, err)
			alice, err := s.TakeoverTarget(t.Context(), "alice", "dance", 101)
			require.NoError(t, err)
			bob, err := s.TakeoverTarget(t.Context(), "alice", "dance", 202)
			require.NoError(t, err)
			assert.Equal(t, "bob", alice.ReceivingAdmin)
			expected := ""
			if partnerState == "paid" {
				expected = "alice"
			}
			assert.Equal(t, expected, bob.ReceivingAdmin)
			assert.Equal(t, "bob", bob.Booking.PaymentAdmin)
			if partnerState == "paid" {
				data, exportErr := s.Export(t.Context(), "alice")
				require.NoError(t, exportErr)
				file := openExport(t, data)
				rows := exportRows(t, file, "Passes")
				assert.Equal(t, "202", rows[1][18])
				assert.Equal(t, "101", rows[2][18], "assignment backfill overrides current-contact fallback")
				receivedAt, cellErr := file.GetCellValue("Passes", "T3")
				require.NoError(t, cellErr)
				assert.Empty(t, receivedAt, "backfill must not invent receipt time")
			}
		})
	}
}

func TestPassTakeoverConcurrentReplayAndNoticeCurrentness(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	db := f.db
	s := passbooking.Service{DB: db, Delivery: f.b.Delivery}
	alice, err := s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`,
	)
	require.NoError(t, err)
	drainPassNotices(t, f)
	c := passbooking.Command{
		Name:          passbooking.CommandTakeover,
		Event:         "dance",
		Key:           "parallel",
		Version:       alice.Version,
		Target:        "alice",
		TargetVersion: alice.Version,
	}
	failures := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() { _, executeErr := s.Execute(t.Context(), "alice", c); failures <- executeErr })
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		require.NoError(t, failure)
	}
	// Recreate the service to prove the notice and replay survive a process restart.
	s = passbooking.Service{DB: db, Delivery: syntheticDeliverySettings()}
	notices, err := s.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.True(t, notices[0].Current)
	assert.Equal(t, "payment_contact_changed", notices[0].Kind)
	require.NoError(t, deferPassTestNotice(t.Context(), s, notices[0]))
	target, err := s.TakeoverTarget(t.Context(), "bob", "dance", 101)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          passbooking.CommandTakeover,
			Event:         "dance",
			Key:           "back",
			Version:       target.ActorVersion,
			Target:        "alice",
			TargetVersion: target.Booking.Version,
		},
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		notices, err = s.PendingNotifications(t.Context())
		return err != nil || len(notices) > 0
	}, 35*time.Second, 100*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.False(t, notices[0].Current, "superseded contact notice is not delivered")
	gate, err := s.BeginNotification(
		t.Context(),
		delivery.Attempt{ID: notices[0].ID, Generation: notices[0].DeliveryAttempt},
	)
	require.NoError(t, err)
	assert.False(t, gate.Ready)
	assert.Equal(t, "notification_no_longer_current", gate.Reason)
	var latest int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT max(id) FROM core.pass_notifications WHERE kind='payment_contact_changed'`).
			Scan(&latest),
	)
	require.NoError(t, f.b.DeliverPassNotification(t.Context(), latest))
	status, err := s.NotificationStatus(t.Context(), latest)
	require.NoError(t, err)
	assert.Equal(t, string(delivery.Succeeded), status.State, "current contact notice is delivered")
	assert.Positive(t, status.MessageID)
	notices, err = s.PendingNotifications(t.Context())
	require.NoError(t, err)
	assert.Empty(t, notices)
}
