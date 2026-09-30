package passbooking

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// readOriginRows fails the first Scan, or only Rows.Err when scan is false.
type readOriginRows struct {
	pgx.Rows

	err  error
	scan bool
}

func (rows *readOriginRows) Next() bool {
	next := rows.scan
	rows.scan = false
	return next
}

func (*readOriginRows) Close() {}

func (rows *readOriginRows) Scan(...any) error { return rows.err }

func (rows *readOriginRows) Err() error { return rows.err }

func TestReadAuthoritySQLOrigins(t *testing.T) {
	t.Parallel()
	for _, input := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(input.Error(), func(t *testing.T) {
			t.Parallel()
			ctx := receiptParentContext(t, input)
			tx := originFailureTx{err: input}
			want := input
			if errors.Is(input, io.EOF) {
				want = core.ErrDatabase
			}
			check := func(err error) {
				t.Helper()
				require.ErrorIs(t, err, want)
				require.NotErrorIs(t, err, io.EOF)
				if errors.Is(want, core.ErrDatabase) {
					require.EqualError(t, err, "database unavailable")
				}
			}
			_, err := capabilitiesInTx(t.Context(), tx, "owner", "dance")
			check(err)
			check(LockDeliveryProofInTx(t.Context(), tx, "admin", "dance", "owner", 1, "attempt"))
			check(LockDeliveryProofInTx(t.Context(), tx, "owner", "dance", "owner", 1, "attempt"))
			_, err = OperationReadAuthorities(ctx, tx, "admin", "dance", commandAdminAssign, []string{"owner"})
			check(err)
			_, err = ExportOperationAuthority(t.Context(), tx, "admin")
			check(err)
			_, err = exportSnapshotEvents(t.Context(), tx, "admin")
			check(err)
			_, err = collectCatalog(&readOriginRows{err: input, scan: true})
			check(err)
			_, err = collectCatalog(&readOriginRows{err: input})
			check(err)
		})
	}
}

func TestReadAuthoritySQLAbsenceAndValidationKeepMeaning(t *testing.T) {
	t.Parallel()
	tx := originFailureTx{err: pgx.ErrNoRows}
	require.Equal(t, forbidden(), LockDeliveryProofInTx(t.Context(), tx, "admin", "dance", "owner", 1, "attempt"))
	require.Equal(t, forbidden(), LockDeliveryProofInTx(t.Context(), tx, "owner", "dance", "owner", 1, "attempt"))
	_, err := OperationReadAuthorities(t.Context(), tx, "admin", "dance", commandAdminAssign, []string{"owner"})
	require.Equal(t, forbidden(), err)
	_, err = ExportOperationAuthority(t.Context(), tx, "admin")
	require.Equal(t, forbidden(), err)

	// Validation precedes SQL; a failing transaction must not reclassify it.
	failing := originFailureTx{err: io.EOF}
	err = LockDeliveryExportInTx(t.Context(), failing, "admin", nil)
	require.Equal(t, invalid(), err)
	require.NotErrorIs(t, err, core.ErrDatabase)
	err = (Service{}).CheckExportSnapshot(t.Context(), "admin", []string{"b", "a"})
	require.Equal(t, invalid(), err)
	require.NotErrorIs(t, err, core.ErrDatabase)
	_, err = (Service{}).AdminTarget(t.Context(), "admin", "dance", 0)
	require.Equal(t, invalid(), err)
	require.NotErrorIs(t, err, core.ErrDatabase)
}
