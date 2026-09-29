package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestPrivilegedPractitionerEscapedScopeContinuation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	event := strings.Repeat(`"`, 200)
	party := strings.Repeat(`\`, 200)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.massage_events(id) VALUES($1)`, event)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_parties(id,event_id,starts_at,ends_at,tables) VALUES($1,$2,now(),now()+interval '1 day',1)`,
		party,
		event,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_specialists(event_id,owner,name) VALUES($1,'alice','Me'),($1,'bob','Other')`,
		event,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_bookings(id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub)
 SELECT 'cursor-'||n,$1,$2,'visitor','alice',n,1,now()+n*interval '1 day',now()+n*interval '1 day'+interval '15 minutes',10,10 FROM generate_series(1,21)n`,
		event,
		party,
	)
	require.NoError(t, err)
	service := massage.Service{DB: f.db}
	first, err := service.PractitionerBookings(t.Context(), "alice", event, party, "")
	require.NoError(t, err)
	require.True(t, first.More)
	require.Len(t, first.Items, 20)
	require.LessOrEqual(
		t,
		len(first.NextCursor),
		2048,
		"every advertised continuation must fit the documented input limit",
	)
	second, err := service.PractitionerBookings(t.Context(), "alice", event, party, first.NextCursor)
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	assert.False(t, second.More)
	assert.NotEqual(t, first.Items[19].ID, second.Items[0].ID)
	for _, filter := range []struct{ actor, event, party string }{
		{"bob", event, party}, {"alice", event, ""}, {"alice", event, party[:199] + "x"}, {"alice", event[:199] + "x", party},
	} {
		_, readErr := service.PractitionerBookings(
			t.Context(),
			filter.actor,
			filter.event,
			filter.party,
			first.NextCursor,
		)
		requireCode(t, readErr, "read_cursor_invalid")
	}
	_, err = service.PractitionerSchedule(t.Context(), "alice", event, first.NextCursor)
	requireCode(t, err, "read_cursor_invalid")
}
