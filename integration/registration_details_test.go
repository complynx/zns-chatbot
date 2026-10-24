package integration_test

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func detailsProtector(t *testing.T, key byte) *registrationdetails.Protector {
	t.Helper()
	protector, err := registrationdetails.NewProtector(bytes.Repeat([]byte{key}, 32))
	require.NoError(t, err)
	return protector
}

func TestRegistrationDetailsKeepProfileSeparateAndRecoverHistoricalReceipt(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	user, _ := registrationUser(t, h, 95001, false)
	scope := ownerProfileScope(t, h, user)
	profileMutation := profileChange("set_name", 0)
	profileMutation.FullName = "Public profile name"
	profile := changeProfile(t, h, scope, profileMutation)
	protector := detailsProtector(t, 61)
	initial, err := h.store.RegistrationDetailsStatus(t.Context(), scope)
	require.NoError(t, err)
	require.Zero(t, initial.Version)
	name := registrationdetails.Mutation{
		OperationID: uuid.New(),
		Field:       registrationdetails.LegalName,
		Value:       "  Synthetic Legal Name  ",
	}
	named, err := h.store.MutateRegistrationDetails(t.Context(), scope, name, protector)
	require.NoError(t, err)
	require.Equal(t, registrationdetails.Status{Version: 1, LegalNamePresent: true}, named)
	name.Value = "Synthetic Legal Name"
	normalizedReplay, err := h.store.MutateRegistrationDetails(t.Context(), scope, name, protector)
	require.NoError(t, err)
	require.Equal(t, named, normalizedReplay)
	passport := registrationdetails.Mutation{
		OperationID:     uuid.New(),
		ExpectedVersion: named.Version,
		Field:           registrationdetails.PassportNumber,
		Value:           "SYNTHETIC-DOC-95001",
	}
	completed, err := h.store.MutateRegistrationDetails(t.Context(), scope, passport, protector)
	require.NoError(t, err)
	require.Equal(
		t,
		registrationdetails.Status{Version: 2, LegalNamePresent: true, PassportNumberPresent: true},
		completed,
	)
	var encrypted, receipt []byte
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), `SELECT encrypted_record FROM shuttle_registration_details WHERE user_id=$1`, user).
			Scan(&encrypted),
	)
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT result FROM shuttle_registration_details_operations WHERE user_id" +
"=$1 AND operation_id=$2", user, passport.OperationID).
			Scan(&receipt),
	)
	for _, raw := range [][]byte{encrypted, receipt} {
		require.NotContains(t, string(raw), name.Value)
		require.NotContains(t, string(raw), passport.Value)
	}
	reopened, err := db.Open(t.Context(), h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	fresh := detailsProtector(t, 61)
	updatedName := name
	updatedName.OperationID, updatedName.ExpectedVersion, updatedName.Value = uuid.New(), completed.Version, "Other Synthetic Legal Name"
	later, err := reopened.MutateRegistrationDetails(t.Context(), scope, updatedName, fresh)
	require.NoError(t, err)
	require.EqualValues(t, 3, later.Version)
	require.True(t, later.PassportNumberPresent)
	replayed, err := reopened.MutateRegistrationDetails(t.Context(), scope, passport, fresh)
	require.NoError(t, err)
	require.Equal(t, completed, replayed)
	historical, err := reopened.RegistrationDetailsOperation(t.Context(), scope, passport.OperationID)
	require.NoError(t, err)
	require.Equal(t, completed, historical)
	currentProfile, err := reopened.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, profile.FullName, currentProfile.FullName)
	require.Equal(t, profile.Version, currentProfile.Version)
	rawProfile, err := json.Marshal(currentProfile)
	require.NoError(t, err)
	require.NotContains(t, string(rawProfile), "passport")
	require.NotContains(t, string(rawProfile), "Legal Name")
}

func TestRegistrationDetailsRejectStaleForeignAndUnprotectedChanges(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	user, _ := registrationUser(t, h, 95002, false)
	other, _ := registrationUser(t, h, 95003, false)
	scope := ownerProfileScope(t, h, user)
	otherScope := ownerProfileScope(t, h, other)
	protector := detailsProtector(t, 62)
	mutation := registrationdetails.Mutation{
		OperationID: uuid.New(),
		Field:       registrationdetails.PassportNumber,
		Value:       "SYNTHETIC-DOC-95002",
	}
	original, err := h.store.MutateRegistrationDetails(t.Context(), scope, mutation, protector)
	require.NoError(t, err)
	changed := mutation
	changed.Value = "OTHER-SYNTHETIC-DOC"
	_, err = h.store.MutateRegistrationDetails(t.Context(), scope, changed, protector)
	require.ErrorIs(t, err, db.ErrConflict)
	changed.OperationID = uuid.New()
	_, err = h.store.MutateRegistrationDetails(t.Context(), scope, changed, protector)
	require.ErrorIs(t, err, db.ErrConflict)
	changed.ExpectedVersion = original.Version
	foreign := otherScope
	foreign.UserID = user
	_, err = h.store.MutateRegistrationDetails(t.Context(), foreign, changed, protector)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.RegistrationDetailsOperation(t.Context(), otherScope, mutation.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.MutateRegistrationDetails(t.Context(), scope, changed, nil)
	require.ErrorIs(t, err, registrationdetails.ErrProtection)
	_, err = h.store.MutateRegistrationDetails(t.Context(), scope, changed, detailsProtector(t, 63))
	require.ErrorIs(t, err, registrationdetails.ErrProtection)
	status, err := h.store.RegistrationDetailsStatus(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, original, status)
	var operations int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), `SELECT count(*) FROM shuttle_registration_details_operations WHERE user_id=$1`, user).
			Scan(&operations),
	)
	require.Equal(t, 1, operations)
}

func TestRegistrationDetailsConcurrentFieldsShareOneVersion(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	user, _ := registrationUser(t, h, 95004, false)
	scope := ownerProfileScope(t, h, user)
	protector := detailsProtector(t, 64)
	changes := []registrationdetails.Mutation{
		{OperationID: uuid.New(), Field: registrationdetails.LegalName, Value: "Synthetic Legal Name"},
		{OperationID: uuid.New(), Field: registrationdetails.PassportNumber, Value: "SYNTHETIC-DOC-95004"},
	}
	var group sync.WaitGroup
	results := make([]error, len(changes))
	for i, change := range changes {
		group.Go(func() { _, results[i] = h.store.MutateRegistrationDetails(t.Context(), scope, change, protector) })
	}
	group.Wait()
	succeeded, conflicted, loser := 0, 0, 0
	for i, err := range results {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, db.ErrConflict)
			conflicted++
			loser = i
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	retry := changes[loser]
	retry.OperationID, retry.ExpectedVersion = uuid.New(), 1
	completed, err := h.store.MutateRegistrationDetails(t.Context(), scope, retry, protector)
	require.NoError(t, err)
	require.Equal(
		t,
		registrationdetails.Status{Version: 2, LegalNamePresent: true, PassportNumberPresent: true},
		completed,
	)
}
