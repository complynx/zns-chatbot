package registrationdetails_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestProtectedDetailsSurviveReopenAndRejectChangedOwnerKeyOrData(t *testing.T) {
	t.Parallel()
	secret := bytes.Repeat([]byte{41}, 32)
	owner := uuid.New()
	protector, err := registrationdetails.NewProtector(secret)
	require.NoError(t, err)
	encrypted, err := protector.Seal(owner, "SYNTHETIC-PASSPORT-123")
	require.NoError(t, err)
	require.NotContains(t, string(encrypted), "SYNTHETIC-PASSPORT-123")
	second, err := protector.Seal(owner, "SYNTHETIC-PASSPORT-123")
	require.NoError(t, err)
	require.NotEqual(t, encrypted, second)
	reopened, err := registrationdetails.NewProtector(secret)
	require.NoError(t, err)
	value, err := reopened.Open(owner, encrypted)
	require.NoError(t, err)
	require.Equal(t, "SYNTHETIC-PASSPORT-123", value)
	other, err := registrationdetails.NewProtector(bytes.Repeat([]byte{42}, 32))
	require.NoError(t, err)
	changed := bytes.Clone(encrypted)
	changed[len(changed)-1] ^= 1
	for _, tc := range []struct {
		name    string
		reader  *registrationdetails.Protector
		owner   uuid.UUID
		payload []byte
	}{
		{"owner", reopened, uuid.New(), encrypted},
		{"key", other, owner, encrypted},
		{"changed", reopened, owner, changed},
		{"truncated", reopened, owner, encrypted[:2]},
		{"empty owner", reopened, uuid.Nil, encrypted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, openErr := tc.reader.Open(tc.owner, tc.payload)
			require.ErrorIs(t, openErr, registrationdetails.ErrProtection)
			require.Empty(t, got)
		})
	}
}

func TestDetailsReceiptFingerprintSurvivesReopenAndSeparatesOwnerAndRequest(t *testing.T) {
	t.Parallel()
	secret := bytes.Repeat([]byte{51}, 32)
	owner := uuid.New()
	request := []byte(`{"operation_id":"fixture","passport_number":"SYNTHETIC-123"}`)
	protector, err := registrationdetails.NewProtector(secret)
	require.NoError(t, err)
	first, err := protector.Fingerprint(owner, request)
	require.NoError(t, err)
	reopened, err := registrationdetails.NewProtector(secret)
	require.NoError(t, err)
	replay, err := reopened.Fingerprint(owner, request)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	differentOwner, err := reopened.Fingerprint(uuid.New(), request)
	require.NoError(t, err)
	require.NotEqual(t, first, differentOwner)
	differentRequest, err := reopened.Fingerprint(owner, append(bytes.Clone(request), ' '))
	require.NoError(t, err)
	require.NotEqual(t, first, differentRequest)
	differentKey, err := registrationdetails.NewProtector(bytes.Repeat([]byte{52}, 32))
	require.NoError(t, err)
	differentReceipt, err := differentKey.Fingerprint(owner, request)
	require.NoError(t, err)
	require.NotEqual(t, first, differentReceipt)
}

func TestProtectionRequiresConfiguredSecretAndOwner(t *testing.T) {
	t.Parallel()
	for _, secret := range [][]byte{nil, make([]byte, 31), make([]byte, 33)} {
		_, err := registrationdetails.NewProtector(secret)
		require.ErrorIs(t, err, registrationdetails.ErrProtection)
	}
	for _, protector := range []*registrationdetails.Protector{nil, {}} {
		_, err := protector.Seal(uuid.New(), "synthetic")
		require.ErrorIs(t, err, registrationdetails.ErrProtection)
		value, err := protector.Open(uuid.New(), []byte("invalid"))
		require.ErrorIs(t, err, registrationdetails.ErrProtection)
		require.Empty(t, value)
		_, err = protector.Fingerprint(uuid.New(), []byte("synthetic"))
		require.ErrorIs(t, err, registrationdetails.ErrProtection)
	}
}
