package integration_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type massageWriteBarrier struct {
	once   sync.Once
	before func()
}

func (barrier *massageWriteBarrier) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPut && request.URL.Path == "/v1/massage/preferences" {
		barrier.once.Do(barrier.before)
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestScriptMassageRoleRevokedAtMutation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.massage_events(id) VALUES('role-only');
INSERT INTO core.massage_specialists(event_id,owner,name) VALUES('role-only','alice','Alice')`)
	require.NoError(t, err)
	f.b.API.HTTP = &http.Client{Transport: &massageWriteBarrier{before: func() {
		_, deleteErr := f.db.Exec(
			t.Context(),
			`DELETE FROM core.massage_specialists WHERE event_id='role-only' AND owner='alice'`,
		)
		require.NoError(t, deleteErr)
	}}}
	result := runMassageScript(t, f, identity.AliceTelegramID, 9954, `
const cached=tools.massage.practitioner.configure;
let denied=false; try {cached({event:"role-only",bookings:false,next:false});} catch (_) {denied=true;}
const listed=tools.$list();
let helpDenied=false; try {cached.$help();} catch (_) {helpDenied=true;}
return {denied,helpDenied,hidden:!listed.some(t=>t.name==="massage.practitioner.configure")};`)
	assert.JSONEq(t, `{"denied":true,"helpDenied":true,"hidden":true}`, string(result))
	var outcome string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content->0->'calls'->0->'outcome'->>'error'
FROM bot.interactions WHERE owner='alice' AND update_id=9954 AND kind='script_runs'`).Scan(&outcome))
	assert.Equal(t, "denied", outcome)
}
