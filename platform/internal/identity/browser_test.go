package identity_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestBrowserTokensHaveSeparateAudienceAndFixedExpiry(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	token := signer.BrowserSession("request", time.Now().Add(time.Hour))
	id, err := signer.VerifyBrowserSession(token)
	require.NoError(t, err)
	assert.Equal(t, "request", id)
	_, err = signer.Verify(token)
	require.Error(t, err)
	_, err = signer.VerifyBrowserSession(signer.Token("request"))
	require.Error(t, err)
	_, err = signer.VerifyBrowserSession(signer.BrowserSession("request", time.Now().Add(-time.Second)))
	require.Error(t, err)
}

func TestBrowserOriginsRejectAmbiguousOrBroadConfiguration(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"*", "https://*.example", "null", "http://example.com", "https://example.com/", "https://user@example.com", "https://example.com https://example.com"} {
		_, err := identity.BrowserOrigins(raw)
		require.Error(t, err, raw)
	}
	origins, err := identity.BrowserOrigins("https://one.example https://two.example:8443")
	require.NoError(t, err)
	assert.Len(t, origins, 2)
	assert.True(t, origins["https://two.example:8443"])
}
