package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRetiredReceiptRealDriverAbsence(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"command", "batch"} {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			service, input := r100CanonicalReceipt(t, family)
			_, err := service.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, err)
			if family == "command" {
				input.Command.Key = "r100-absent"
				witness, witnessErr := passbooking.CommandOperationWitness("bob", *input.Command)
				require.NoError(t, witnessErr)
				input.Witness = &witness
			} else {
				input.Batch.Key = "r100-absent"
				witness, witnessErr := passbooking.BatchOperationWitness("bob", *input.Batch)
				require.NoError(t, witnessErr)
				input.Witness = &witness
			}
			stored := r100ReceiptSnapshot(t, service.DB)
			got, err := service.ReadPassOperation(t.Context(), "bob", input)
			require.NotErrorIs(t, err, core.ErrDatabase)
			if family == "command" {
				require.NoError(t, err)
				require.Equal(t, "not_committed", got.Summary.Status)
			} else {
				requireCode(t, err, "pass_operation_unavailable")
				require.Equal(t, derivedmutation.PassOperationRead{}, got)
			}
			require.Equal(t, stored, r100ReceiptSnapshot(t, service.DB))
		})
	}
}

func TestRetiredReceiptRealDriverStoredEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, statement, code string
	}{
		{"plan shape", `UPDATE core.pass_admin_batches SET plan='[]'::jsonb WHERE actor='bob'`, ""},
		{"before shape", `UPDATE core.pass_admin_assignments SET before_records='{}'::jsonb WHERE actor='bob'`, ""},
		{"after shape", `UPDATE core.pass_admin_assignments SET after_records='{}'::jsonb WHERE actor='bob'`, ""},
		{"plan mismatch", `UPDATE core.pass_admin_batches SET plan='{}'::jsonb WHERE actor='bob'`, "source_stale"},
		{"missing assignment", `DELETE FROM core.pass_admin_assignments WHERE actor='bob'`, "source_stale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service, input := r100CanonicalReceipt(t, "batch")
			before, err := service.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, err)
			require.Equal(t, "complete", before.Summary.Status)
			require.Equal(t, 1, before.Summary.Committed)
			changed, err := service.DB.Exec(t.Context(), test.statement)
			require.NoError(t, err)
			require.EqualValues(t, 1, changed.RowsAffected())
			stored := r100ReceiptSnapshot(t, service.DB)
			got, err := service.ReadPassOperation(t.Context(), "bob", input)
			require.Error(t, err)
			require.NotErrorIs(t, err, core.ErrDatabase)
			if test.code == "" {
				var shape *json.UnmarshalTypeError
				require.ErrorAs(t, err, &shape)
			} else {
				requireCode(t, err, test.code)
			}
			require.Equal(t, derivedmutation.PassOperationRead{}, got)
			require.Equal(t, stored, r100ReceiptSnapshot(t, service.DB))
		})
	}
}
