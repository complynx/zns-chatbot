package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassNotificationsPaymentRequestSurvivingParticipant(t *testing.T) {
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
	drainPassDomainNotices(t, service)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("synthetic couple"))
	require.NoError(t, err)
	submit := bookingCommand("proof", "proof", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	cancel := bookingCommand("admin_cancel", "cancel-submitter", bob)
	cancel.Target, cancel.TargetVersion = "alice", alice.Version
	bob, err = service.Execute(t.Context(), "bob", cancel)
	require.NoError(t, err)
	notice := pendingPaymentRequest(t, service)
	assert.True(t, notice.Current, "the surviving participant still needs review")
	assert.Equal(t, "bob", notice.Owner)
	assert.Equal(t, bob.Version, notice.Version)
	queue, err := service.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
	assert.Equal(t, queue.Items[0].Payment.Attempt, notice.Attempt)

	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='bob'`)
	require.NoError(t, err)
	assert.False(t, pendingPaymentRequest(t, service).Current, "revoked administrator cannot receive the notice")
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','bob')`)
	require.NoError(t, err)
	assert.True(t, pendingPaymentRequest(t, service).Current)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_notifications SET recipient='alice' WHERE kind='payment_request';
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice')`)
	require.NoError(t, err)
	assert.False(
		t,
		pendingPaymentRequest(t, service).Current,
		"another current admin is not the immutable receiving admin",
	)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_notifications SET recipient='bob' WHERE kind='payment_request'`)
	require.NoError(t, err)
	cancel = bookingCommand("admin_cancel", "cancel-survivor", bob)
	cancel.Target, cancel.TargetVersion = "bob", bob.Version
	_, err = service.Execute(t.Context(), "bob", cancel)
	require.NoError(t, err)
	assert.False(t, pendingPaymentRequest(t, service).Current, "no attached participant remains")
}

func pendingPaymentRequest(t *testing.T, service passbooking.Service) passbooking.Notification {
	// Reconstruct a later poll without sleeping; this helper only inspects current domain eligibility.
	_, resetErr := service.DB.Exec(
		t.Context(),
		`UPDATE core.pass_notifications SET lease_until=NULL WHERE delivery_state='pending'`,
	)
	require.NoError(t, resetErr)
	t.Helper()
	for range 10 {
		notices, err := service.PendingNotifications(t.Context())
		require.NoError(t, err)
		for _, notice := range notices {
			if notice.Kind == "payment_request" {
				return notice
			}
			require.NoError(t, cancelPassTestNotice(t, service, notice))
		}
	}
	t.Fatal("payment request not found")
	return passbooking.Notification{}
}
