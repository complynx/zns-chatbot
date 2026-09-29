package identity_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestSignedPrincipal(t *testing.T) {
	t.Parallel()
	s := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	token := s.Token("alice")
	got, err := s.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "alice", got)
	for _, bad := range []string{"", token + "x", "bad.bad", identity.Signer{Key: []byte(strings.Repeat("x", 32))}.Token("alice")} {
		_, err = s.Verify(bad)
		require.Error(t, err, "accepted invalid token")
	}
	_, err = (identity.Signer{}).Verify(token)
	require.Error(t, err, "empty key")
}
func FuzzVerify(f *testing.F) {
	f.Add("malformed.token")
	s := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	f.Fuzz(func(_ *testing.T, token string) { _, _ = s.Verify(token) })
}
