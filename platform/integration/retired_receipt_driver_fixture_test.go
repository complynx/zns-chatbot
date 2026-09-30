package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func r100CanonicalReceipt(t *testing.T, family string) (derivedmutation.Service, derivedmutation.PassOperationInput) {
	t.Helper()
	if family == "proof" {
		return r100ProofReceipt(t)
	}
	db, registration := adminPairFixture(t)
	service := derivedmutation.Service{DB: db, Registration: registration}
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation, PrivateHistory: true, Authorities: []readsource.Authority{},
	}
	input := derivedmutation.PassOperationInput{Retired: true}
	switch family {
	case "command", "command fallback":
		input = retiredWitnessFixture(t, service, "command", source)
	case "assignment", "batch":
		input = retiredWitnessFixture(t, service, family, source)
	case "takeover", "takeover fallback":
		command := passbooking.Command{
			Name: passbooking.CommandTakeover, Event: "dance", Key: "r100-takeover",
			Version: 1, Target: "alice", TargetVersion: 1,
		}
		witness, err := passbooking.CommandOperationWitness("bob", command)
		require.NoError(t, err)
		_, err = service.ExecutePassBooking(t.Context(), "bob", command, source)
		require.NoError(t, err)
		input.Command, input.Witness = &command, &witness
	case "cancel batch":
		batch := passbooking.RuntimeBatch{
			Event: "dance", Key: "r100-cancel-batch", Action: "admin_cancel", Recipients: []int64{101},
		}
		witness, err := passbooking.BatchOperationWitness("bob", batch)
		require.NoError(t, err)
		_, err = service.RunPassBatch(t.Context(), "bob", batch, source)
		require.NoError(t, err)
		input.Batch, input.Witness = &batch, &witness
	default:
		t.Fatalf("unknown receipt fixture %q", family)
	}
	if family == "command fallback" || family == "takeover fallback" {
		_, err := db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
		require.NoError(t, err)
	}
	return service, input
}

func r100ProofReceipt(t *testing.T) (derivedmutation.Service, derivedmutation.PassOperationInput) {
	t.Helper()
	db, registration := bookingFixture(t)
	alice, err := registration.Execute(t.Context(), "alice", bookingCommand("solo", "r100-solo", passbooking.Booking{}))
	require.NoError(t, err)
	require.Equal(t, "assigned", alice.State)
	proof, err := (orders.Service{DB: db}).UploadProof(
		t.Context(),
		"alice",
		"r100-proof.txt",
		[]byte("synthetic proof"),
	)
	require.NoError(t, err)
	submit := bookingCommand("proof", "r100-submit", alice)
	submit.ProofID = proof.ID
	alice, err = registration.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	payment, err := registration.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	command := bookingCommand("proof_accept", "r100-review", passbooking.Booking{})
	command.Target, command.TargetVersion, command.PaymentAttempt = "alice", alice.Version, payment.Attempt
	witness, err := passbooking.CommandOperationWitness("bob", command)
	require.NoError(t, err)
	_, err = registration.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	return derivedmutation.Service{DB: db, Registration: registration}, derivedmutation.PassOperationInput{
		Retired: true, Command: &command, Witness: &witness,
	}
}
