package appservices

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistrationRetentionReachesDerivedCopies(t *testing.T) {
	t.Parallel()
	services := NewServices(nil, Options{RegistrationRetention: 2 * time.Minute})
	require.Equal(t, 2*time.Minute, services.Registration.RegistrationRetention)
	require.Equal(t, 2*time.Minute, services.DerivedMutations.Registration.RegistrationRetention)
	require.Equal(t, 2*time.Minute, services.AdminUtilities.Registration.RegistrationRetention)
}
