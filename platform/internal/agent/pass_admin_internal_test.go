package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationAssignmentSchemaAndOptions(t *testing.T) {
	t.Parallel()
	const valid = `{"name":"admin_assign","event":"dance","target":"alice","invite_telegram_id":0,"payment_admin":"","view":"","cursor":"","assignment":{"total_price":0,"kind":null,"comment":null,"skip_balance":null,"append_tier":null,"create":false,"from_profile":false,"role":"","legal_name":null}}`
	require.NoError(t, codexRegistrationFields([]byte(valid)))
	require.NoError(t, validatePlanFields([]byte(valid), planRegistrationField))
	zero, negative, tier, skip, name := 0, -1, 1, true, "Alice Smith"
	for _, tc := range []struct {
		name    string
		options RegistrationAssignment
		valid   bool
	}{
		{"default", RegistrationAssignment{}, true},
		{"free", RegistrationAssignment{TotalPrice: &zero}, true},
		{"negative", RegistrationAssignment{TotalPrice: &negative}, false},
		{"exclusive", RegistrationAssignment{SkipBalance: &skip, AppendTier: &tier}, false},
		{"profile", RegistrationAssignment{Create: true, FromProfile: true}, true},
		{"explicit", RegistrationAssignment{Create: true, Role: "follower", LegalName: &name}, true},
		{"mixed_identity", RegistrationAssignment{Create: true, FromProfile: true, LegalName: &name}, false},
		{"implicit_create", RegistrationAssignment{LegalName: &name}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, validRegistrationAssignment(&tc.options))
		})
	}
}
