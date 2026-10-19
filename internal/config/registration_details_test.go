package config_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestRegistrationDetailsKeyIsOptionalAndEnvironmentOverridesYAML(t *testing.T) {
	t.Setenv("SHUTTLE_REGISTRATION_DETAILS_KEY", "")
	unset, err := config.Load(configFile(t, ""))
	require.NoError(t, err)
	secret, err := unset.RegistrationDetailsSecret()
	require.NoError(t, err)
	require.Empty(t, secret)
	yamlKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{71}, 32))
	envSecret := bytes.Repeat([]byte{72}, 32)
	t.Setenv("SHUTTLE_REGISTRATION_DETAILS_KEY", base64.RawURLEncoding.EncodeToString(envSecret))
	loaded, err := config.Load(configFile(t, "\nregistration_details_key: "+yamlKey+"\n"))
	require.NoError(t, err)
	secret, err = loaded.RegistrationDetailsSecret()
	require.NoError(t, err)
	require.Equal(t, envSecret, secret)
}

func TestRegistrationDetailsKeyRejectsMalformedInputWithoutDisclosure(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{73}, 32))
	for _, value := range []string{"synthetic-key-do-not-print", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{74}, 31)), encoded + "=", " " + encoded} {
		t.Run(value[:4], func(t *testing.T) {
			t.Setenv("SHUTTLE_REGISTRATION_DETAILS_KEY", value)
			_, err := config.Load(configFile(t, ""))
			require.Error(t, err)
			require.NotContains(t, err.Error(), value)
			require.NotContains(t, err.Error(), encoded)
		})
	}
}
