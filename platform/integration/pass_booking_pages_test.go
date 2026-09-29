package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassBookingPagesSurviveBoundaryDeletion(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
 SELECT 'invite-'||n,1000+n,'Synthetic '||n,true FROM generate_series(1,121) AS n;
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,invitation_target,payment_admin,created_at)
 SELECT 'dance',id,1,'waiting-for-couple','leader','solo',202,'bob',now()
 FROM core.users WHERE id LIKE 'invite-%'`)
	require.NoError(t, err)
	cursor := ""
	seen := map[string]bool{}
	for {
		page, pageErr := service.Invitations(t.Context(), "bob", "dance", cursor)
		require.NoError(t, pageErr)
		require.LessOrEqual(t, len(page.Invitations), 25)
		for _, invitation := range page.Invitations {
			require.False(t, seen[invitation.From.Owner], "page must not repeat an invitation")
			seen[invitation.From.Owner] = true
		}
		if cursor == "" {
			require.Len(t, page.Invitations, 25)
			_, err = db.Exec(
				t.Context(),
				`DELETE FROM core.pass_bookings WHERE owner=$1`,
				page.Invitations[24].From.Owner,
			)
			require.NoError(t, err)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	assert.Len(t, seen, 121)
	foreign, err := service.Invitations(t.Context(), "visitor", "dance", cursor)
	require.NoError(t, err)
	assert.Empty(t, foreign.Invitations)
	_, err = service.Invitations(t.Context(), "bob", "dance", "not-a-cursor")
	requireCode(t, err, "pass_booking_invalid")

	count := 0
	cursor = ""
	for {
		page, pageErr := service.Queue(t.Context(), "bob", "dance", cursor)
		require.NoError(t, pageErr)
		require.LessOrEqual(t, len(page.Bookings), 25)
		require.Len(t, page.Names, len(page.Bookings))
		for _, booking := range page.Bookings {
			assert.NotEmpty(t, page.Names[booking.Owner])
		}
		count += len(page.Bookings)
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	assert.Equal(t, 120, count)
	_, err = service.Queue(t.Context(), "alice", "dance", cursor)
	requireCode(t, err, "forbidden")
}
