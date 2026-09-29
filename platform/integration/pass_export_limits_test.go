package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassExportRowAndInputBounds(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name                string
		rows, commentLength int
	}{
		{"rows", 10001, 0}, {"bytes", 9000, 2000},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			db, service := bookingFixture(t)
			_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name)
 SELECT 'export-'||i,100000+i,'Export fixture' FROM generate_series(1,$1::integer) i`, scenario.rows)
			require.NoError(t, err)
			_, err = db.Exec(
				t.Context(),
				`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,comment)
 SELECT 'dance',id,1,'waitlist','leader','solo','bob',now(),repeat('x',$1) FROM core.users WHERE id LIKE 'export-%'`,
				scenario.commentLength,
			)
			require.NoError(t, err)
			body, err := service.Export(t.Context(), "bob")
			requireCode(t, err, "export_too_large")
			assert.Empty(t, body, "no truncated workbook may escape")
		})
	}
}
