package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestPersonalizationTokenConstruction(t *testing.T) {
	t.Parallel()
	source, err := personalizationToken(config.Config{}, nil)
	require.NoError(t, err)
	require.Nil(t, source)
	// The general startup validator need not duplicate machine-token validation.
	c := config.Config{Loom: config.Loom{Personalization: config.PersonalizationIdentity{
		Subject: "billing", ClientID: "client", ClientSecret: "secret",
	}}}
	source, err = personalizationToken(c, nil)
	require.Error(t, err, "invalid machine configuration must fail during construction, not on a delivery")
	require.Nil(t, source)
}
