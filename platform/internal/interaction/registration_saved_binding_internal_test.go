package interaction

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestSavedRegistrationKeysPreserveIdentity(t *testing.T) {
	t.Parallel()
	for _, explicit := range []string{"", "original-explicit-key"} {
		t.Run("command/"+explicit, func(t *testing.T) {
			t.Parallel()
			plan := SavedPlan{RegistrationCommand: &passbooking.Command{Key: explicit}}
			require.NoError(t, bindSavedRegistrationKeys(42, &plan))
			want := explicit
			if want == "" {
				want = "tg-registration-42"
			}
			require.Equal(t, want, plan.RegistrationCommand.Key)
			require.NoError(t, bindSavedRegistrationKeys(99, &plan))
			require.Equal(t, want, plan.RegistrationCommand.Key)
		})
		t.Run("assignment/"+explicit, func(t *testing.T) {
			t.Parallel()
			plan := SavedPlan{RegistrationAssignment: &passbooking.AdminAssignment{Key: explicit}}
			require.NoError(t, bindSavedRegistrationKeys(42, &plan))
			want := explicit
			if want == "" {
				want = "tg-admin-assignment-42"
			}
			require.Equal(t, want, plan.RegistrationAssignment.Key)
			require.NoError(t, bindSavedRegistrationKeys(99, &plan))
			require.Equal(t, want, plan.RegistrationAssignment.Key)
		})
	}
}
