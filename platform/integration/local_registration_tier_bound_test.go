package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
)

func TestLocalRegistrationTierResponseBound(t *testing.T) {
	t.Parallel()
	db, _, local, remote := registrationOperationsFixture(t)
	for _, count := range []int{6000, 12000, 14000} {
		_, err := db.Exec(t.Context(), `DELETE FROM core.pass_event_tiers WHERE event_id='dance'`)
		require.NoError(t, err)
		_, err = db.Exec(t.Context(), `
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 SELECT 'dance',i,1,1,'2026-01-01'::timestamptz FROM generate_series(0,$1::int-1) i`, count)
		require.NoError(t, err)
		value, readErr := local.PassTierStatus(t.Context(), "bob", "dance")
		remoteValue, remoteErr := remote.PassTierStatus(t.Context(), "bob", "dance")
		if count == 6000 {
			require.NoError(t, readErr)
			require.NoError(t, remoteErr)
			require.Len(t, value.Tiers, count)
			require.Equal(t, remoteValue, value)
		} else {
			requireCode(t, readErr, "read_result_limit")
			requireCode(t, remoteErr, "read_result_limit")
			require.Empty(t, value)
			require.Empty(t, remoteValue)
		}
	}
	for _, client := range []appclient.Client{local, remote} {
		value, err := client.PassTierStatus(t.Context(), "alice", "dance")
		requireCode(t, err, "forbidden")
		require.Empty(t, value)
	}
}
