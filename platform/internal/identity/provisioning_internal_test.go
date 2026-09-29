package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTelegramProvisioningTokenBindsPurposeBodyBotAndLifetime(t *testing.T) {
	t.Parallel()
	signer := Signer{Key: []byte(strings.Repeat("k", MinKeyBytes))}
	body := []byte(`{"bot_id":77,"user":{"id":101}}`)
	token := signer.TelegramProvisioningToken(77, body)
	require.NoError(t, signer.VerifyTelegramProvisioning(token, 77, body))
	for _, invalid := range []string{signer.Token("101"), signer.DeliveryToken(), signer.MemoryProvenanceToken("101"),
		signer.tokenUntil(provisioningDigest(77, body), telegramProvisioningAudience, time.Now().Add(-time.Second)),
		signer.tokenUntil(provisioningDigest(77, body), telegramProvisioningAudience, time.Now().Add(time.Hour))} {
		require.Error(t, signer.VerifyTelegramProvisioning(invalid, 77, body))
	}
	require.Error(t, signer.VerifyTelegramProvisioning(token, 88, body))
	require.Error(t, signer.VerifyTelegramProvisioning(token, 77, []byte(`{"bot_id":77,"user":{"id":202}}`)))
	require.Error(t, signer.VerifyTelegramProvisioning(token, 77, []byte(strings.Repeat("x", MaxProvisioningBytes+1))))
}
