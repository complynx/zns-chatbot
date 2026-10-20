package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/migrations"
)

func TestMiniAppPairAssignmentMigrationPreservesCanonicalAndNativeHistory(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "before-native-journal", args)
	require.NoError(t, err)
	driver := stdlib.OpenDBFromPool(f.H.store.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 63, false)
	require.NoError(t, err)
	recovered, err := f.H.store.PreparePairAssignment(ctx, admin, "before-native-journal", args)
	require.NoError(t, err)
	require.Equal(t, prep, recovered, "An empty journal rollback must retain canonical proposals")
	_, err = provider.ApplyVersion(ctx, 63, true)
	require.NoError(t, err)
	session := nativePairAssignmentSession(t, f, admin)
	adopted, err := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "native-after-up", args, prep.ID, session)
	require.NoError(t, err)
	require.Equal(t, prep, adopted)
	_, err = provider.ApplyVersion(ctx, 63, false)
	require.ErrorContains(t, err, "native pair assignment actions")
	recent, err := f.H.store.RecentMiniAppPairAssignmentProposals(ctx, admin, session)
	require.NoError(t, err)
	require.Len(t, recent.Proposals, 1)
	require.Equal(t, prep, recent.Proposals[0].Preparation)
	// The refused rollback retains the alias, journal and no business effects.
	recovered, err = f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "native-after-up", args, uuid.Nil, session)
	require.NoError(t, err)
	require.Equal(t, prep, recovered)
	requireNativePairAssignmentEffects(t, f, admin, 0, 0, 1)
}
