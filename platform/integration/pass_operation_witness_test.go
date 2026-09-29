package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPassOperationRetiredWitnessReceipts(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"command", "assignment", "batch"} {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			db, registration := adminPairFixture(t)
			service := derivedmutation.Service{DB: db, Registration: registration}
			generation := int64(0)
			source := readsource.Derivation{
				Generation:     &generation,
				PrivateHistory: true,
				Authorities:    []readsource.Authority{},
			}
			input := retiredWitnessFixture(t, service, family, source)
			var before int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&before),
			)
			value, err := service.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, err)
			require.Nil(t, value.Summary.Context)
			require.Equal(t, "unavailable", value.Summary.Continuation)
			require.NotEmpty(t, value.ReadAuthorities)
			want := "committed"
			if family == "batch" {
				want = "complete"
				require.Equal(t, 1, value.Summary.Committed)
			}
			require.Equal(t, want, value.Summary.Status)
			raw, err := json.Marshal(value)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "PRIVATE-WITNESS-COMMENT")
			missing := input
			missing.Witness = nil
			_, err = service.ReadPassOperation(t.Context(), "bob", missing)
			requireCode(t, err, "pass_operation_unavailable")
			changed := *input.Witness
			changed.Owner = "alice"
			foreign := input
			foreign.Witness = &changed
			_, err = service.ReadPassOperation(t.Context(), "bob", foreign)
			requireCode(t, err, "pass_operation_unavailable")
			_, err = db.Exec(
				t.Context(),
				`DELETE FROM core.pass_booking_admins WHERE owner='bob'; DELETE FROM core.pass_payment_admins WHERE owner='bob'`,
			)
			require.NoError(t, err)
			denied, err := service.ReadPassOperation(t.Context(), "bob", input)
			require.Error(t, err)
			require.Equal(t, derivedmutation.PassOperationRead{}, denied)
			var after int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&after),
			)
			require.Equal(t, before, after, "receipt inspection must not execute or rebuild intent")
		})
	}
}

func retiredWitnessFixture(
	t *testing.T,
	service derivedmutation.Service,
	family string,
	source readsource.Derivation,
) derivedmutation.PassOperationInput {
	t.Helper()
	input := derivedmutation.PassOperationInput{Retired: true}
	comment := "PRIVATE-WITNESS-COMMENT"
	var witness passbooking.OperationWitness
	var err error
	switch family {
	case "command":
		c := passbooking.Command{
			Name:          "admin_cancel",
			Event:         "dance",
			Key:           "witness-command",
			Version:       1,
			Target:        "alice",
			TargetVersion: 1,
		}
		witness, err = passbooking.CommandOperationWitness("bob", c)
		require.NoError(t, err)
		_, err = service.ExecutePassBooking(t.Context(), "bob", c, source)
		input.Command = &c
	case "assignment":
		c := passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "witness-assignment",
			Version:       1,
			Target:        "alice",
			TargetVersion: 1,
			Comment:       &comment,
		}
		witness, err = passbooking.AssignmentOperationWitness("bob", c)
		require.NoError(t, err)
		_, err = service.AssignPass(t.Context(), "bob", c, source)
		c.Comment = nil
		input.Assignment = &c
	case "batch":
		c := passbooking.RuntimeBatch{
			Event:      "dance",
			Key:        "witness-batch",
			Action:     "admin_assign",
			Recipients: []int64{101},
			Options:    passbooking.AdminAssignment{Comment: &comment},
		}
		witness, err = passbooking.BatchOperationWitness("bob", c)
		require.NoError(t, err)
		_, err = service.RunPassBatch(t.Context(), "bob", c, source)
		c.Options.Comment = nil
		input.Batch = &c
	}
	require.NoError(t, err)
	input.Witness = &witness
	return input
}
