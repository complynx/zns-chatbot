package sandbox

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationFixtureControlsAreBounded(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"read", "revoke-payment-a", "revoke-booking-admin"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := RegistrationFixture{Action: action, Stand: RegistrationFixtureStand}
			require.NoError(t, f.validate())
			f.OpensAt = time.Now()
			require.Error(t, f.validate())
		})
	}
	for _, f := range []RegistrationFixture{
		{Action: "init", Stand: RegistrationFixtureStand},
		{Action: "read", Stand: "other-stand"},
		{Action: "grant", Stand: RegistrationFixtureStand},
		{Action: "advance-time", Stand: RegistrationFixtureStand},
	} {
		require.Error(t, f.validate())
	}
	require.NoError(
		t,
		(RegistrationFixture{Action: "init", Stand: RegistrationFixtureStand, OpensAt: time.Now()}).validate(),
	)
}

func TestRegistrationFixtureReadbackHasOnlySafeFields(t *testing.T) {
	t.Parallel()
	state := RegistrationFixtureState{
		Rows: []RegistrationFixtureRow{{Event: RegistrationFixtureEventA, Owner: "alice", Actions: []string{"solo"}}},
	}
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	assert.Len(t, fields, 2)
	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fields["rows"], &rows))
	assert.Len(t, rows[0], 3)
	assert.Contains(t, rows[0], "event")
	assert.Contains(t, rows[0], "owner")
	assert.Contains(t, rows[0], "actions")
}
