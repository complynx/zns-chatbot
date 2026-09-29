package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassCapabilitiesCurrentRolesAndEventIsolation(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('other',now()+interval '1 day')`,
	)
	require.NoError(t, err)
	ordinary, err := service.Capabilities(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.ElementsMatch(
		t,
		[]string{"solo", "invite", "accept", "decline", "payment_admin", "cancel"},
		ordinary.Actions,
	)
	visitor, err := service.Capabilities(t.Context(), "visitor", "dance")
	require.NoError(t, err)
	assert.Empty(t, visitor.Actions)
	globalOther, err := service.Capabilities(t.Context(), "bob", "other")
	require.NoError(t, err)
	assert.Contains(t, globalOther.Actions, "admin_assign")
	assert.Contains(t, globalOther.Actions, "takeover")
	assert.NotContains(t, globalOther.Actions, "proof_accept")
	assert.NotContains(t, globalOther.Actions, "proof_reject")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	payment, err := service.Capabilities(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Contains(t, payment.Actions, "proof_accept")
	assert.Contains(t, payment.Actions, "admin_cancel")
	assert.Contains(t, payment.Actions, "takeover")
	assert.NotContains(t, payment.Actions, "admin_assign")
	assert.NotContains(t, payment.Actions, "admin_uncouple")
	other, err := service.Capabilities(t.Context(), "bob", "other")
	require.NoError(t, err)
	assert.ElementsMatch(t, ordinary.Actions, other.Actions)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	revoked, err := service.Capabilities(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.ElementsMatch(t, ordinary.Actions, revoked.Actions)
}
