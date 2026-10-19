package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/migrations"
)

func TestPairAssignmentAliasMigrationPreservesCanonicalHistory(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), admin, "original", args)
	require.NoError(t, err)
	driver := stdlib.OpenDBFromPool(f.H.store.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(t.Context(), 62, false)
	require.NoError(t, err)
	var canonicalCount int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_pair_preparations WHERE proposal_id=$1", prep.ID).
			Scan(&canonicalCount),
	)
	require.Equal(t, 1, canonicalCount)
	_, err = provider.ApplyVersion(t.Context(), 62, true)
	require.NoError(t, err)
	adopted, err := f.H.store.AdoptPairAssignment(t.Context(), admin, "alias", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, prep, adopted)
	_, err = provider.ApplyVersion(t.Context(), 62, false)
	require.ErrorContains(t, err, "pair assignment aliases")
	recovered, err := f.H.store.PreparePairAssignment(t.Context(), admin, "alias", args)
	require.NoError(t, err)
	require.Equal(t, prep, recovered)
	var aliases, passes int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_pair_assignment_aliases").Scan(&aliases),
	)
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Equal(t, 1, aliases)
	require.Zero(t, passes)
}
