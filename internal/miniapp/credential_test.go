package miniapp_test

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/miniapp"
)

func TestApplicationBearerStableRetryAndKeyRotation(t *testing.T) {
	t.Parallel()
	encoded := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("n", 32)))
	nonce, err := miniapp.DecodeNonce(encoded)
	require.NoError(t, err)
	session := uuid.MustParse("915dbe23-c021-4f14-9147-c3c3e74dc4fc")
	key := miniapp.NewSessionKey(fixtureToken)
	token := applicationBearer(t, key, fixtureBot, session, nonce)
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)
	require.Len(t, decoded, sha256.Size)
	restarted := miniapp.NewSessionKey(fixtureToken)
	require.Equal(t, token, applicationBearer(t, restarted, fixtureBot, session, nonce))
	require.Equal(t, key.Generation(), restarted.Generation())
	rotated := miniapp.NewSessionKey("6087685431:rotated_secret")
	require.NotEqual(t, token, applicationBearer(t, rotated, fixtureBot, session, nonce))
	require.NotEqual(t, key.Generation(), rotated.Generation())
	require.NotEqual(t, token, applicationBearer(t, key, fixtureBot+1, session, nonce))
	require.NotEqual(t, token, applicationBearer(t, key, fixtureBot, uuid.New(), nonce))
	nonce[0]++
	require.NotEqual(t, token, applicationBearer(t, key, fixtureBot, session, nonce))
	for _, invalid := range []string{"", encoded + "=", encoded + "\n", strings.Repeat("a", 42), strings.Repeat("a", 44)} {
		_, decodeErr := miniapp.DecodeNonce(invalid)
		require.ErrorIs(t, decodeErr, miniapp.ErrInvalid)
	}
	_, err = key.Bearer(-1, session, nonce)
	require.ErrorIs(t, err, miniapp.ErrInvalid)
	_, err = key.Bearer(fixtureBot, uuid.Nil, nonce)
	require.ErrorIs(t, err, miniapp.ErrInvalid)
}

func applicationBearer(t *testing.T, key miniapp.SessionKey, bot int64, session uuid.UUID, nonce [32]byte) string {
	t.Helper()
	token, err := key.Bearer(bot, session, nonce)
	require.NoError(t, err)
	return token
}
