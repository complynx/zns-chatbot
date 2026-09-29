package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDerivedMutationTokenSeparatesAudiences(t *testing.T) {
	t.Parallel()
	signer := Signer{Key: []byte(strings.Repeat("k", MinKeyBytes))}
	token := signer.DerivedMutationToken("alice")
	owner, err := signer.VerifyDerivedMutation(token)
	require.NoError(t, err)
	require.Equal(t, "alice", owner)
	_, err = signer.Verify(token)
	require.ErrorIs(t, err, ErrSandboxIdentity)
	_, err = signer.VerifyMemoryProvenance(token)
	require.ErrorIs(t, err, ErrSandboxIdentity)
	require.Error(t, signer.VerifyDelivery(token))
	for _, invalid := range []string{
		"", signer.Token("alice"), signer.MemoryProvenanceToken("alice"), signer.DeliveryToken(),
		signer.DerivedMutationToken(""), token + "tampered",
		signer.tokenUntil("alice", derivedMutationAudience, time.Now().Add(-time.Second)),
	} {
		_, err = signer.VerifyDerivedMutation(invalid)
		require.ErrorIs(t, err, ErrSandboxIdentity)
	}
}
