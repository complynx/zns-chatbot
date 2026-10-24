package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func embeddedHistory(t *testing.T, h *harness, scope db.ProfileScope) (uuid.UUID, db.HistoryApplyRequest) {
	t.Helper()
	user := historyUser(42)
	user["old.event"] = map[string]any{
		"state": "paid",
		"type":  "solo",
		"role":  "leader",
		"paid":  false,
		"bank":  "PRIVATE_BANK",
	}
	user["unsupported"] = map[string]any{"paid": "yes"}
	raw, digest := historyBundle(t, "embedded-history", map[string][]map[string]any{"users": {user}})
	args := saveHistoryBundle(t, h.store, scope, raw, digest)
	r := prepareHistory(t, h.store, scope, args)
	_, err := h.store.ApplyHistory(context.Background(), scope, r, func(context.Context) error { return nil })
	require.NoError(t, err)
	page, err := h.store.OwnerHistory(context.Background(), scope, uuid.Nil, 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	return page.Items[0].ID, r
}

func TestHistoricalRegistrationAPIReadsClaimsWithoutChangingHistory(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, owner := registrationUser(t, h, 42, true)
	_, foreignAdmin := registrationUser(t, h, 43, true)
	scope := historyScope(t, h, id)
	record, applied := embeddedHistory(t, h, scope)
	ctx := context.Background()
	before, err := h.store.HistoryApplyOperation(ctx, scope, applied.OperationID)
	require.NoError(t, err)
	pageBefore, err := h.store.OwnerHistory(ctx, scope, uuid.Nil, 10)
	require.NoError(t, err)
	read := func(token, key string) (int, []byte) {
		return adminRequest(t, h, token, http.MethodPost, "/internal/agent/history/registration", map[string]any{
			"protocol_version": 1,
			"operation_id":     "read-registration",
			"arguments":        map[string]any{"record_id": record, "pass_key": key},
		})
	}
	for _, tc := range []struct{ key, status string }{
		{"old.event", "claim"},
		{"missing", "missing"},
		{"unsupported", "unsupported"},
	} {
		code, body := read(owner, tc.key)
		require.Equal(t, http.StatusOK, code, string(body))
		var response struct {
			Result db.HistoryRegistration `json:"result"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, tc.status, response.Result.Status)
		require.Equal(t, applied.Arguments.ArchiveID, response.Result.ArchiveID)
		require.Equal(t, applied.Arguments.SHA256, response.Result.ArchiveSHA256)
		require.NotContains(t, string(body), "PRIVATE_BANK")
		if tc.status == "claim" {
			require.Equal(t, "paid", response.Result.Fields.State)
			require.NotNil(t, response.Result.Fields.PaidClaim)
			require.False(t, *response.Result.Fields.PaidClaim)
		} else {
			require.Nil(t, response.Result.Fields)
		}
	}
	code, body := read(foreignAdmin, "old.event")
	require.Equal(t, http.StatusNotFound, code, string(body))
	after, err := h.store.HistoryApplyOperation(ctx, scope, applied.OperationID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	pageAfter, err := h.store.OwnerHistory(ctx, scope, uuid.Nil, 10)
	require.NoError(t, err)
	require.Equal(t, pageBefore, pageAfter)
	_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET tg_user_id=44 WHERE id=$1", id)
	require.NoError(t, err)
	code, body = read(owner, "old.event")
	require.Equal(t, http.StatusNotFound, code, string(body))
	_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", id)
	require.NoError(t, err)
	code, body = read(owner, "old.event")
	require.Equal(t, http.StatusForbidden, code, string(body))
}

func TestHistoricalRegistrationUsesOneConnectionAndDiscardsDeniedResult(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	record, _ := embeddedHistory(t, h, scope)
	u, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	q := u.Query()
	q.Set("pool_max_conns", "1")
	u.RawQuery = q.Encode()
	store, err := db.Open(context.Background(), u.String())
	require.NoError(t, err)
	t.Cleanup(store.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	claim, err := store.OwnerHistoryRegistration(
		ctx,
		scope,
		record,
		"old.event",
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.Equal(t, "claim", claim.Status)
	denied := errors.New("IAM denied")
	claim, err = store.OwnerHistoryRegistration(
		ctx,
		scope,
		record,
		"old.event",
		func(context.Context) error { return denied },
	)
	require.ErrorIs(t, err, denied)
	require.Equal(t, db.HistoryRegistration{}, claim)
	cancel()
	claim, err = store.OwnerHistoryRegistration(
		ctx,
		scope,
		record,
		"old.event",
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, db.HistoryRegistration{}, claim)
}

func TestHistoricalRegistrationRejectsDamagedSourceWithoutReturningClaim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, sql string
		status    int
	}{
		{"hash", "UPDATE shuttle_legacy_history_records SET canonical_hash=decode(repeat('00',32)," +
			"'hex') WHERE id=$1", http.StatusConflict},
		{"membership", "UPDATE shuttle_legacy_history_memberships SET source_line=2 WHERE record_id=$1", http.StatusNotFound},
		{"archive", "UPDATE shuttle_legacy_archive_chunks SET content=set_byte(content," +
			"octet_length(content)-8,get_byte(content,octet_length(content)-8)#1) WHERE " +
			"archive_id=(SELECT first_archive_id FROM shuttle_legacy_history_records WHERE id=$1)", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := setup(t, newPeers(t))
			id, token := registrationUser(t, h, 42, true)
			record, _ := embeddedHistory(t, h, historyScope(t, h, id))
			_, err := h.store.Pool().Exec(context.Background(), tc.sql, record)
			require.NoError(t, err)
			code, body := adminRequest(
				t,
				h,
				token,
				http.MethodPost,
				"/internal/agent/history/registration",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     "damaged-registration",
					"arguments":        map[string]any{"record_id": record, "pass_key": "old.event"},
				},
			)
			require.Equal(t, tc.status, code, string(body))
			require.NotContains(t, string(body), `"fields"`)
			require.NotContains(t, string(body), "PRIVATE_BANK")
			code, body = discoverHistory(t, h, token, record, "", 1)
			require.Equal(t, tc.status, code, string(body))
			require.NotContains(t, string(body), `"items"`)
			require.NotContains(t, string(body), "PRIVATE_BANK")
		})
	}
}
