package integration_test

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func TestLocalRegistrationTierManyBookings(t *testing.T) {
	t.Parallel()
	// A child running only this test isolates allocation accounting from other tests.
	if os.Getenv("ZNS_TIER_RESOURCE_CHILD") != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		child := exec.CommandContext(ctx, executable, "-test.run=^TestLocalRegistrationTierManyBookings$", "-test.v")
		child.Env = append(os.Environ(), "ZNS_TIER_RESOURCE_CHILD=1")
		output, err := child.CombinedOutput()
		require.NoError(t, err, "%s", output)
		t.Log(string(output))
		return
	}
	db, _, local, remote := registrationOperationsFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,name,telegram_id)
 SELECT 'tier-resource-'||i,'Synthetic',100000+i FROM generate_series(1,40000) i;
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index,skip_balance,comment)
 SELECT 'dance','tier-resource-'||i,1,
 CASE WHEN i<=1000 THEN 'assigned' WHEN i<=2000 THEN 'paid' WHEN i<=3000 THEN 'waitlist' ELSE 'cancelled' END,
 CASE WHEN i%2=0 THEN 'leader' ELSE 'follower' END,'solo','bob',now(),
 CASE WHEN i<=2000 THEN now() END,CASE WHEN i<=2000 THEN CASE WHEN i%4=0 THEN 0 ELSE 25 END END,
 CASE WHEN i<=2000 THEN i%3 END,CASE WHEN i%3=0 THEN true WHEN i%3=1 THEN false END,repeat('x',2000)
 FROM generate_series(1,40000) i`)
	require.NoError(t, err)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	value, err := local.PassTierStatus(t.Context(), "bob", "dance")
	runtime.ReadMemStats(&after)
	t.Logf(
		"tier read allocated %d bytes with 40000 rows and 80000000 comment bytes",
		after.TotalAlloc-before.TotalAlloc,
	)
	require.NoError(t, err)
	require.Less(
		t,
		after.TotalAlloc-before.TotalAlloc,
		uint64(24<<20),
		"tier read must not materialize booking comments/history",
	)
	require.Equal(t, passallocation.Counts{Leader: 1000, Follower: 1000}, value.Participants)
	require.Equal(t, map[int]int{0: 666, 1: 667, 2: 667}, value.Usage)
	remoteValue, err := remote.PassTierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, value, remoteValue)
	for _, client := range []appclient.Client{local, remote} {
		denied, readErr := client.PassTierStatus(t.Context(), "alice", "dance")
		requireCode(t, readErr, "forbidden")
		require.Empty(t, denied)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		canceled, readErr := client.PassTierStatus(ctx, "bob", "dance")
		require.ErrorIs(t, readErr, context.Canceled)
		require.Empty(t, canceled)
	}
}

func TestLocalRegistrationTierUnconfiguredIndexBound(t *testing.T) {
	t.Parallel()
	db, _, local, remote := registrationOperationsFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,name,telegram_id)
 SELECT 'tier-index-'||i,'Synthetic',200000+i FROM generate_series(1,72000) i;
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index)
 SELECT 'dance','tier-index-'||i,1,'paid','leader','solo','bob',now(),now(),25,1000000000+i FROM generate_series(1,72000) i`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		value, readErr := client.PassTierStatus(t.Context(), "bob", "dance")
		requireCode(t, readErr, "read_result_limit")
		require.Empty(t, value)
	}
}
