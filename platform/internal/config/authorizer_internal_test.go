package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthorizerRequiresExplicitTrustedRelayConfiguration(t *testing.T) {
	t.Parallel()
	a := Authorizer{Issuer: "https://relay.invalid", JWKSURL: "https://relay.invalid/jwks", IDPID: "relay-idp"}
	require.NoError(t, a.validate(productionMode))
	a.JWKSURL = "http://localhost:8114/jwks"
	require.Error(t, a.validate(productionMode))
	require.NoError(t, a.validate(sandboxMode))
	a.JWKSURL = "http://remote.invalid/jwks"
	require.Error(t, a.validate(sandboxMode))
	a.JWKSURL = "https://user:secret@relay.invalid/jwks"
	require.Error(t, a.validate(productionMode))
	a.JWKSURL = "https://relay.invalid/jwks"
	a.IDPID = ""
	require.Error(t, a.validate(productionMode))
}
