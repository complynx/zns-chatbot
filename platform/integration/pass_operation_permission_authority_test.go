package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassOperationTargetWitnessMapping(t *testing.T) {
	t.Parallel()
	for _, replacement := range []bool{false, true} {
		name := "remapped"
		if replacement {
			name = "replacement_principal"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			tx, err := f.db.Begin(t.Context())
			require.NoError(t, err)
			refs, err := passbooking.OperationReadAuthorities(
				t.Context(),
				tx,
				"bob",
				"dance",
				"admin_assign",
				[]string{"alice"},
			)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(t.Context()))
			check := func(want bool) {
				readTx, beginErr := f.db.Begin(t.Context())
				require.NoError(t, beginErr)
				valid, readErr := passbooking.LockReadAuthorities(t.Context(), readTx, "bob", refs)
				require.NoError(t, readErr)
				require.NoError(t, readTx.Rollback(t.Context()))
				require.Equal(t, []bool{true, want}, valid)
			}
			check(true)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET telegram_id=900101 WHERE id='alice'`)
			require.NoError(t, err)
			if replacement {
				_, err = f.db.Exec(
					t.Context(),
					`INSERT INTO core.users(id,telegram_id,name,can_book) VALUES('synthetic-replacement',101,'Synthetic replacement',true)`,
				)
				require.NoError(t, err)
			}
			check(false)
		})
	}
}

func TestPassExportMetadataPermissionWitness(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	tx, err := f.db.Begin(t.Context())
	require.NoError(t, err)
	refs, err := passbooking.ExportOperationAuthority(t.Context(), tx, "bob")
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	require.Len(t, refs, 1)
	require.Equal(t, passbooking.ReadExportPermission, refs[0].Kind)
	require.NotEmpty(t, refs[0].Event)
	require.Empty(t, refs[0].Owner)
	require.Zero(t, refs[0].Version)
	check := func(want bool) {
		readTx, beginErr := f.db.Begin(t.Context())
		require.NoError(t, beginErr)
		valid, readErr := passbooking.LockReadAuthorities(t.Context(), readTx, "bob", refs)
		require.NoError(t, readErr)
		require.NoError(t, readTx.Rollback(t.Context()))
		require.Equal(t, []bool{want}, valid)
	}
	check(true)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins WHERE owner='bob'; DELETE FROM core.pass_payment_admins WHERE owner='bob'`,
	)
	require.NoError(t, err)
	check(false)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES($1,'bob')`,
		refs[0].Event,
	)
	require.NoError(t, err)
	check(true)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='bob'`)
	require.NoError(t, err)
	check(false)
}
