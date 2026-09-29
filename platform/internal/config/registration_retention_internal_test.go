package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistrationRetentionConfiguration(t *testing.T) {
	t.Parallel()
	cfg, err := Load(modeHealth, []byte("env: sandbox\n"), nil)
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, cfg.Registration.Retention)
	cfg, err = Load(modeHealth, []byte("env: sandbox\n"), []string{"ZNS_REGISTRATION__RETENTION=3m"})
	require.NoError(t, err)
	require.Equal(t, 3*time.Minute, cfg.Registration.Retention)
	for _, value := range []string{"0s", "-1m", "1ns", "invalid"} {
		_, err = Load(modeHealth, []byte("env: sandbox\nregistration:\n  retention: "+value+"\n"), nil)
		require.Error(t, err)
	}
}
