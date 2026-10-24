package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestParticipantPairAPIReadsOnlyOwnMetadataAndReconcilesDissolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	owner, partner := pairUserToken(f, f.A), pairUserToken(f, f.B)
	other, administrator := pairUserToken(f, f.Other), pairUserToken(f, admin)
	read := map[string]any{"protocol_version": 1, "operation_id": "read-my-pairs", "arguments": map[string]any{}}
	var target db.OwnedPair
	for _, token := range []string{owner, partner} {
		code, data := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/my", read)
		require.Equal(t, http.StatusOK, code, string(data))
		var response struct {
			Result struct {
				Pairs []db.OwnedPair `json:"pairs"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(data, &response))
		require.Len(t, response.Result.Pairs, 1)
		target = response.Result.Pairs[0]
		require.Equal(t, args.PairID, target.Pair.ID)
		require.Equal(t, "Europe/Minsk", target.Timezone)
		require.NotContains(t, string(data), "price_minor")
		require.NotContains(t, string(data), "confirmed_minor")
	}
	for _, token := range []string{other, administrator} {
		code, data := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/my", read)
		require.Equal(t, http.StatusOK, code, string(data))
		require.Contains(t, string(data), `"pairs":[]`)
	}
	write := map[string]any{
		"protocol_version": 1,
		"operation_id":     "participant-unpair",
		"idempotency_key":  "participant-unpair",
		"arguments": db.PairDissolution{
			EventID:         target.Pair.EventID,
			PairID:          target.Pair.ID,
			ExpectedVersion: target.Pair.Version,
		},
		"confirmation": map[string]any{"mode": "none"},
	}
	code, data := adminRequest(t, f.H, administrator, http.MethodPost, "/internal/agent/pairs/dissolve", write)
	require.Equal(t, http.StatusForbidden, code, string(data))
	_, err := f.H.store.Pool().Exec(ctx, "UPDATE shuttle_events SET modules='{}' WHERE id=$1", f.EventID)
	require.NoError(t, err)
	code, data = adminRequest(t, f.H, owner, http.MethodPost, "/internal/agent/pairs/my", read)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), args.PairID.String())
	code, data = adminRequest(t, f.H, owner, http.MethodPost, "/internal/agent/pairs/dissolve", write)
	require.Equal(t, http.StatusOK, code, string(data))
	var executed struct {
		Result registrations.Pair `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &executed))
	require.Equal(t, "dissolved", executed.Result.State)
	for _, token := range []string{owner, partner} {
		code, data = adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/my", read)
		require.Equal(t, http.StatusOK, code, string(data))
		require.Contains(t, string(data), `"pairs":[]`)
	}
	f.H.stop(t)
	f.H.start(t)
	status := map[string]any{"protocol_version": 1, "operation_id": "participant-unpair"}
	for _, suffix := range []string{"status", "reconcile"} {
		code, data = adminRequest(t, f.H, owner, http.MethodPost, "/internal/agent/pairs/dissolve/"+suffix, status)
		require.Equal(t, http.StatusOK, code, string(data))
		var replay struct {
			Result registrations.Pair `json:"result"`
		}
		require.NoError(t, json.Unmarshal(data, &replay))
		require.Equal(t, executed.Result, replay.Result)
	}
	code, data = adminRequest(t, f.H, partner, http.MethodPost, "/internal/agent/pairs/dissolve/status", status)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
	require.NoError(t, f.H.store.RevokeUser(ctx, f.A.UserID, true))
	code, data = adminRequest(t, f.H, owner, http.MethodPost, "/internal/agent/pairs/dissolve/status", status)
	require.Equal(t, http.StatusForbidden, code, string(data))
}
