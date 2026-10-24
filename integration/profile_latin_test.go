package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func withLatinName(m db.ProfileMutation, latinFirst, latinLast string) db.ProfileMutation {
	m.FirstNameLatin, m.LastNameLatin = &latinFirst, &latinLast
	return m
}

func TestProfileLatinAtomicCorrectionsReceiptsAndIAMOriginals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19301, false)
	scope := ownerProfileScope(t, h, userID)
	first := withLatinName(personName(0, "Анна", "Иванова"), "Anna", "Ivanova")
	saved := changeProfile(t, h, scope, first)
	require.Equal(t, "Анна Иванова", saved.FullName)
	require.Equal(t, "Anna", saved.FirstNameLatin)
	require.Equal(t, "Ivanova", saved.LastNameLatin)
	reopened, err := db.Open(ctx, h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	persisted, err := reopened.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, saved.FirstNameLatin, persisted.FirstNameLatin)
	require.Equal(t, saved.LastNameLatin, persisted.LastNameLatin)
	require.NoError(t, h.store.DispatchProfileName(ctx, scope, func(value db.ProfileNameSync) error {
		require.Equal(t, "Анна", value.FirstName)
		require.Equal(t, "Иванова", value.LastName)
		return nil
	}))
	corrected := changeProfile(
		t,
		h,
		scope,
		withLatinName(personName(saved.Version, "Анна", "Иванова"), "Anna", "Yvanova"),
	)
	require.Equal(t, saved.Version+1, corrected.Version)
	ready, err := h.store.ReadyProfileNames(ctx, 123, "org")
	require.NoError(t, err)
	require.Empty(t, ready, "Latin spelling corrections must not enqueue IAM work")
	unchanged := changeProfile(t, h, scope, personName(corrected.Version, "Анна", "Иванова"))
	require.Equal(t, corrected.Version, unchanged.Version)
	require.Equal(t, "Yvanova", unchanged.LastNameLatin)
	cleared := changeProfile(t, h, scope, personName(unchanged.Version, "Анна", "Петрова"))
	require.Empty(t, cleared.FirstNameLatin)
	require.Empty(t, cleared.LastNameLatin)
	require.Equal(t, saved, changeProfile(t, h, scope, first), "Replay retains original Latin spelling and observation")
	collision := first
	other := "Anne"
	collision.FirstNameLatin = &other
	_, err = h.store.MutateProfile(ctx, scope, collision)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.MutateProfile(
		ctx,
		scope,
		withLatinName(personName(saved.Version, "Анна", "Иванова"), "Anne", "Ivanova"),
	)
	require.ErrorIs(t, err, db.ErrConflict)
	scope.Subject = "not-the-owner"
	_, err = h.store.MutateProfile(ctx, scope, first)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Even an existing receipt requires the current owner")
}

func TestProfileLatinSecondWriteFailureRollsBackOriginalAndReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19302, false)
	scope := ownerProfileScope(t, h, userID)
	_, err := h.store.Pool().
		Exec(ctx, "CREATE FUNCTION reject_latin_fixture() RETURNS trigger LANGUAGE plpgsql " +
"AS $$ BEGIN RAISE EXCEPTION 'Latin storage unavailable'; END $$;\nCREATE " +
"TRIGGER reject_latin_fixture BEFORE UPDATE OF first_name_latin,last_name" +
"_latin ON shuttle_user_profiles FOR EACH ROW EXECUTE FUNCTION reject_lat" +
"in_fixture()")
	require.NoError(t, err)
	m := withLatinName(personName(0, "Анна", "Иванова"), "Anna", "Ivanova")
	_, err = h.store.MutateProfile(ctx, scope, m)
	require.Error(t, err)
	current, err := h.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Zero(t, current.Version)
	require.Empty(t, current.FirstName)
	require.Empty(t, current.FirstNameLatin)
	_, err = h.store.ProfileOperation(ctx, scope, m.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var count int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_profile_name_sync WHERE user_id=$1`, userID).
			Scan(&count),
	)
	require.Zero(t, count)
	_, err = h.store.Pool().
		Exec(ctx, `DROP TRIGGER reject_latin_fixture ON shuttle_user_profiles; DROP FUNCTION reject_latin_fixture()`)
	require.NoError(t, err)
	saved := changeProfile(t, h, scope, m)
	require.Equal(t, "Ivanova", saved.LastNameLatin)
	// An older writer knows only original columns. The database clears its stale Latin.
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_user_profiles SET first_name='Анна',last_name='Петрова',f" +
"ull_name='Анна Петрова',version=version+1 WHERE user_id=$1", userID)
	require.NoError(t, err)
	current, err = h.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Empty(t, current.FirstNameLatin)
	require.Empty(t, current.LastNameLatin)
	clearing := withLatinName(personName(current.Version, "Анна", "Петрова"), "Anna", "Petrova")
	saved = changeProfile(t, h, scope, clearing)
	saved = changeProfile(t, h, scope, withLatinName(personName(saved.Version, "Анна", "Петрова"), "", ""))
	require.Empty(t, saved.FirstNameLatin)
	require.Empty(t, saved.LastNameLatin)
	_, err = h.store.Pool().Exec(ctx, `UPDATE shuttle_users SET revoked=true WHERE id=$1`, userID)
	require.NoError(t, err)
	_, err = h.store.MutateProfile(ctx, scope, m)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProfileLatinIAMImportedNameConfirmationDoesNotQueueRemoteWrite(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19304, false)
	allowExistingIAMNameImport(t, h, userID)
	scope := ownerProfileScope(t, h, userID)
	imported, err := h.store.ImportIAMProfileName(ctx, scope, func(context.Context) (string, string, error) {
		return "Анна", "Иванова", nil
	})
	require.NoError(t, err)
	require.Equal(t, "iam", imported.NameSource)
	m := withLatinName(personName(imported.Version, imported.FirstName, imported.LastName), "Anna", "Yvanova")
	saved := changeProfile(t, h, scope, m)
	require.Equal(t, imported.Version+1, saved.Version)
	require.Equal(t, "user", saved.NameSource)
	require.Equal(t, imported.FullName, saved.FullName)
	require.Equal(t, "Yvanova", saved.LastNameLatin)
	var queued int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_profile_name_sync WHERE user_id=$1`, userID).
			Scan(&queued),
	)
	require.Zero(t, queued, "Source confirmation and Latin spelling do not change remote original names")
	corrected := changeProfile(t, h, scope,
		withLatinName(personName(saved.Version, imported.FirstName, imported.LastName), "Anna", "Ivanova"))
	require.Equal(t, saved.Version+1, corrected.Version)
	require.Equal(t, saved, changeProfile(t, h, scope, m), "Replay keeps the original Latin receipt after correction")
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_profile_name_sync WHERE user_id=$1`, userID).
			Scan(&queued),
	)
	require.Zero(t, queued)
}
