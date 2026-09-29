package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassMenuCancellationAvailability(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"waitlist", "assigned", "paid", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_bookings
 (event_id,owner,version,state,role,kind,price,payment_admin,created_at,assigned_at)
 VALUES('dance','alice',1,$1,'leader','solo',0,'bob',now(),
 CASE WHEN $1 IN ('assigned','paid') THEN now() END)`, state)
			require.NoError(t, err)
			handle(t, f.b, message(1, 101, "/passes"))
			handle(t, f.b, passMenuClick(t, f, 101, 2, "Dance"))
			var available bool
			for _, row := range passMenuCard(t, f, 101).Markup.Rows {
				for _, button := range row {
					available = available || button.Text == "Cancel registration"
				}
			}
			assert.Equal(t, state == "waitlist" || state == "assigned", available)
		})
	}
}
