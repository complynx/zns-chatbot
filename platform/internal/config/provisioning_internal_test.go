package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvisioningCredentialsBelongToCoreOnly(t *testing.T) {
	t.Parallel()
	c := productionConfig(modeBot)
	c.Auth.Zitadel.Provisioning = Provisioning{Enabled: true}
	require.NoError(t, c.Validate(modeBot))
	require.ErrorContains(t, c.Validate(modeAPI), "core provisioning requires")
	c.Auth.Zitadel.Provisioning = Provisioning{
		Enabled:        true,
		OrganizationID: "org",
		EmailDomain:    "telegram.invalid",
		ClientID:       "provisioner",
		ClientSecret:   "independent-provisioning-secret",
	}
	require.NoError(t, c.Validate(modeAPI))
	c.Auth.Zitadel.Provisioning.ClientID = c.Auth.Zitadel.ActorClientID
	require.ErrorContains(t, c.Validate(modeAPI), "separate client credentials")
	c.Auth.Zitadel.Provisioning.ClientID = "provisioner"
	c.Auth.Zitadel.Provisioning.EmailDomain = "deliverable.example.com"
	require.ErrorContains(t, c.Validate(modeAPI), "reserved .invalid")
}
