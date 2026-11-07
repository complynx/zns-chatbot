package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestAgentEventReplacementPreservesAssignedPassAndMoney(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	f.H.stop(t)
	const snapshot = `SELECT jsonb_build_object(
'pass',(SELECT to_jsonb(p) FROM shuttle_passes p WHERE id=$1),
'registrations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM shuttle_registrations r),
'evidence',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM shuttle_payment_evidence e),
'reviews',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM shuttle_payment_reviews v))::text`
	var before string
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), snapshot, f.Pass.ID).Scan(&before))
	args := db.EventChange{ID: f.Event.ID, ExpectedVersion: f.Event.Version, Document: f.Event.Document}
	args.Document.Name = "Full configuration replaced after assignment"
	args.Document.Timezone = "Asia/Yekaterinburg"
	args.Document.Modules = []string{"orders"}
	args.Document.Registration, args.Document.Deposit = nil, nil
	body, _ := prepareEventBody(t, f.H, f.Admin, "preserve-assigned-pass", args)
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	changed := eventResult(t, raw)
	require.Equal(t, f.Event.Version+1, changed.Version)
	require.Nil(t, changed.Document.Registration)
	require.Nil(t, changed.Document.Deposit)
	require.Equal(t, args.Document.Modules, changed.Document.Modules)
	var after string
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), snapshot, f.Pass.ID).Scan(&after))
	require.Equal(t, before, after, "Configuration replacement must preserve frozen pass terms and business rows")
}
