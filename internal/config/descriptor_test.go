package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestDescriptorSecretComesFromItsOwnEnvironment(t *testing.T) {
	t.Setenv("SHUTTLE_DESCRIPTOR_CLIENT_SECRET", "synthetic-descriptor-secret")
	c, err := config.Load(configFile(t, "\ndescriptor:\n  subject: descriptor\n  client_id: descriptor-client\n"))
	require.NoError(t, err)
	require.Equal(t, "descriptor", c.Descriptor.Subject)
	require.Equal(t, "descriptor-client", c.Descriptor.ClientID)
	require.Equal(t, "synthetic-descriptor-secret", c.Descriptor.ClientSecret)
	require.NotEqual(t, c.Identity.ActorClientSecret, c.Descriptor.ClientSecret)
}
