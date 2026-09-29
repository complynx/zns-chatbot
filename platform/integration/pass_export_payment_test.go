package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassExportImmutablePaymentReceiver(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("synthetic receipt"))
	require.NoError(t, err)
	command := bookingCommand("proof", "receipt", alice)
	command.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	payment, err := service.Payment(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	command = bookingCommand("proof_accept", "accept", passbooking.Booking{})
	command.Target, command.TargetVersion, command.PaymentAttempt = "alice", alice.Version, payment.Attempt
	_, err = service.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET payment_admin='alice' WHERE owner='alice'`)
	require.NoError(t, err)
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	file := openExport(t, body)
	rows := exportRows(t, file, "Passes")
	require.Len(t, rows, 2)
	assert.Equal(t, "101", rows[1][17])
	assert.Equal(t, "202", rows[1][18])
	assert.NotEmpty(t, rows[1][19])
	assert.NotEmpty(t, rows[1][20])
	assert.Empty(t, rows[1][21])
	assert.Equal(t, proof.ID, rows[1][22])
}

func TestPassExportFreePassMarker(t *testing.T) {
	t.Parallel()
	_, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	price := 0
	_, err = service.AdminAssign(t.Context(), "bob", passbooking.AdminAssignment{
		Event: "dance", Key: "free-export", Target: "alice", TargetVersion: alice.Version, TotalPrice: &price,
	})
	require.NoError(t, err)
	payment, err := service.Payment(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, "free", payment.Kind)
	require.NotNil(t, payment.ReviewedAt)
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rows := exportRows(t, openExport(t, body), "Passes")
	require.Len(t, rows, 2)
	assert.Equal(t, "0", rows[1][12])
	assert.Equal(t, "0", rows[1][13])
	assert.Equal(t, payment.ReviewedAt.UTC().Format("2006-01-02 15:04:05.000"), rows[1][20])
	marker, err := openExport(t, body).GetCellValue("Passes", "W2")
	require.NoError(t, err)
	assert.Equal(t, "free_pass", marker)
}

func TestPassExportPreservesRejectionWithinAssignment(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	var rejectedAt string
	for _, decision := range []string{"reject", "accept"} {
		proof, uploadErr := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte(decision))
		require.NoError(t, uploadErr)
		submit := bookingCommand("proof", "submit-"+decision, alice)
		submit.ProofID = proof.ID
		alice, err = service.Execute(t.Context(), "alice", submit)
		require.NoError(t, err)
		payment, paymentErr := service.Payment(t.Context(), "bob", "dance", "alice")
		require.NoError(t, paymentErr)
		review := bookingCommand("proof_"+decision, decision, passbooking.Booking{})
		review.Target, review.TargetVersion, review.PaymentAttempt = "alice", alice.Version, payment.Attempt
		_, err = service.Execute(t.Context(), "bob", review)
		require.NoError(t, err)
		alice, err = service.Get(t.Context(), "alice", "dance")
		require.NoError(t, err)
		if decision == "reject" {
			err = db.QueryRow(t.Context(), `SELECT to_char(reviewed_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI:SS.MS')
 FROM core.pass_payment_attempts WHERE id=$1`, payment.Attempt).Scan(&rejectedAt)
			require.NoError(t, err)
		}
	}
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rows := exportRows(t, openExport(t, body), "Passes")
	require.Len(t, rows, 2)
	assert.NotEmpty(t, rows[1][20])
	assert.Equal(t, rejectedAt, rows[1][21])
	_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET assigned_at=assigned_at+interval '1 second'
 WHERE event_id='dance' AND owner='alice'`)
	require.NoError(t, err)
	body, err = service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rejected, err := openExport(t, body).GetCellValue("Passes", "V2")
	require.NoError(t, err)
	assert.Empty(t, rejected, "a different assignment must not inherit the rejection")
}
