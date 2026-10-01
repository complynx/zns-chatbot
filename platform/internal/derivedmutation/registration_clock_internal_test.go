package derivedmutation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type commitRegistrationClock struct{ now time.Time }

func (c *commitRegistrationClock) Now(context.Context) (time.Time, error) { return c.now, nil }

type advancingRegistrationPrepared struct {
	sourcePrepared

	clock *commitRegistrationClock
}

func (p *advancingRegistrationPrepared) Apply(ctx context.Context) (int, error) {
	p.clock.now = p.clock.now.Add(time.Microsecond)
	return p.sourcePrepared.Apply(ctx)
}

func TestRegistrationOuterCommitRetainsClockAuthority(t *testing.T) {
	t.Parallel()
	clock := &commitRegistrationClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	scoped, attempt := (passbooking.Service{RegistrationClock: clock}).WithClockAttempt()
	_, err := scoped.RegistrationClock.Now(t.Context())
	require.NoError(t, err)
	tx := &sourceCommitTx{}
	prepared := &advancingRegistrationPrepared{clock: clock}
	generation := int64(0)
	value, err := commitRegistrationPrepared(t.Context(), tx, "alice",
		readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}, prepared, attempt)
	require.ErrorIs(t, err, passbooking.ErrRegistrationTimeChanged)
	require.Zero(t, value)
	require.True(t, prepared.applied)
	require.False(t, tx.committed)
	require.False(t, nativeRegistrationRefusal(err), "native intake remains pending, not rejected")
}
