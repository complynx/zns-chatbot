package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func discoverHistory(t *testing.T, h *harness, token string, record uuid.UUID, after string, limit int) (int, []byte) {
	t.Helper()
	return adminRequest(t, h, token, http.MethodPost, "/internal/agent/history/registrations", map[string]any{
		"protocol_version": 1,
		"operation_id":     "discover-history",
		"arguments":        map[string]any{"record_id": record, "after_key": after, "limit": limit},
	})
}

func TestHistoricalRegistrationDiscoveryPagesOwnerClaimsWithoutWrites(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, owner := registrationUser(t, h, 42, true)
	_, foreign := registrationUser(t, h, 43, true)
	scope := historyScope(t, h, id)
	user := historyUser(42)
	user["a.event"] = map[string]any{"state": "paid", "paid": false, "bank": "PRIVATE_BANK"}
	user["z"] = map[string]any{"role": "admin"}
	user["Я"] = map[string]any{"paid": true}
	user["unsupported-secret"] = map[string]any{"state": "paid", "paid": "PRIVATE_PROOF"}
	raw, digest := historyBundle(t, "discovery-history", map[string][]map[string]any{"users": {user}})
	args := saveHistoryBundle(t, h.store, scope, raw, digest)
	r := prepareHistory(t, h.store, scope, args)
	_, err := h.store.ApplyHistory(t.Context(), scope, r, func(context.Context) error { return nil })
	require.NoError(t, err)
	before, err := h.store.OwnerHistory(t.Context(), scope, uuid.Nil, 10)
	require.NoError(t, err)
	require.Len(t, before.Items, 1)
	record := before.Items[0].ID
	var keys []string
	after := ""
	for {
		code, body := discoverHistory(t, h, owner, record, after, 1)
		require.Equal(t, http.StatusOK, code, string(body))
		require.NotContains(t, string(body), "PRIVATE_")
		require.NotContains(t, string(body), "unsupported-secret")
		var decoded struct {
			Result db.HistoryRegistrations `json:"result"`
		}
		require.NoError(t, json.Unmarshal(body, &decoded))
		page := decoded.Result
		require.Equal(t, record, page.RecordID)
		require.Equal(t, args.ArchiveID, page.ArchiveID)
		require.Equal(t, digest, page.ArchiveSHA256)
		require.Len(t, page.Items, 1)
		item := page.Items[0]
		keys = append(keys, item.PassKey)
		if item.PassKey == "a.event" {
			require.NotNil(t, item.Fields.PaidClaim)
			require.False(t, *item.Fields.PaidClaim)
		}
		if !page.HasMore {
			require.Empty(t, page.NextKey)
			break
		}
		after = page.NextKey
	}
	require.Equal(t, []string{"a.event", "z", "Я"}, keys)
	code, body := discoverHistory(t, h, owner, record, "", 20)
	require.Equal(t, http.StatusOK, code, string(body))
	var completePage struct {
		Result db.HistoryRegistrations `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &completePage))
	require.Len(t, completePage.Result.Items, 3)
	require.False(t, completePage.Result.HasMore)
	code, body = discoverHistory(t, h, foreign, record, "", 1)
	require.Equal(t, http.StatusNotFound, code, string(body))
	afterPage, err := h.store.OwnerHistory(t.Context(), scope, uuid.Nil, 10)
	require.NoError(t, err)
	require.Equal(t, before, afterPage)
	receipt, err := h.store.HistoryApplyOperation(t.Context(), scope, r.OperationID)
	require.NoError(t, err)
	require.Equal(t, args.ArchiveID, receipt.ArchiveID)
	var passes int
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET tg_user_id=44 WHERE id=$1", id)
	require.NoError(t, err)
	code, body = discoverHistory(t, h, owner, record, "", 1)
	require.Equal(t, http.StatusNotFound, code, string(body))
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", id)
	require.NoError(t, err)
	code, body = discoverHistory(t, h, owner, record, "", 1)
	require.Equal(t, http.StatusForbidden, code, string(body))
}

func TestHistoricalRegistrationDiscoveryUsesOneConnectionAndDiscardsDeniedPage(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	record, _ := embeddedHistory(t, h, scope)
	u, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := u.Query()
	query.Set("pool_max_conns", "1")
	u.RawQuery = query.Encode()
	store, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(store.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	page, err := store.OwnerHistoryRegistrations(ctx, scope, record, "", 0, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	denied := errors.New("synthetic IAM denial")
	page, err = store.OwnerHistoryRegistrations(
		ctx,
		scope,
		record,
		"",
		1,
		func(context.Context) error { return denied },
	)
	require.ErrorIs(t, err, denied)
	require.Equal(t, db.HistoryRegistrations{}, page)
	cancel()
	page, err = store.OwnerHistoryRegistrations(ctx, scope, record, "", 1, func(context.Context) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, db.HistoryRegistrations{}, page)
}

func TestHistoricalRegistrationDiscoveryHonorsResponseBudgetAndRejectsWideRecords(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, token := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	user := historyUser(42)
	for i := range legacyimport.MaxRegistrationPageSize {
		key := strings.Repeat("\x01", 254) + strconv.Itoa(10+i)
		user[key] = map[string]any{
			"state": strings.Repeat("\x01", 256),
			"type":  strings.Repeat("\x02", 256),
			"role":  strings.Repeat("\x03", 256),
		}
	}
	raw, digest := historyBundle(t, "discovery-budget", map[string][]map[string]any{"users": {user}})
	args := saveHistoryBundle(t, h.store, scope, raw, digest)
	r := prepareHistory(t, h.store, scope, args)
	_, err := h.store.ApplyHistory(t.Context(), scope, r, func(context.Context) error { return nil })
	require.NoError(t, err)
	page, err := h.store.OwnerHistory(t.Context(), scope, uuid.Nil, 10)
	require.NoError(t, err)
	record := page.Items[0].ID
	code, body := discoverHistory(t, h, token, record, "", 20)
	require.Equal(t, http.StatusOK, code)
	manifestCode, manifestBody := adminRequest(t, h, token, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, manifestCode)
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID        string `json:"id"`
				MaxResult int    `json:"max_result_bytes"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(manifestBody, &manifest))
	budget := 0
	for _, module := range manifest.Modules {
		if module.ID == "zinusya_history" {
			for _, method := range module.Methods {
				if method.ID == "registrations" {
					budget = method.MaxResult
				}
			}
		}
	}
	require.Positive(t, budget)
	require.LessOrEqual(t, len(body), budget)
	var decoded struct {
		Result db.HistoryRegistrations `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Len(t, decoded.Result.Items, 20)
	code, _ = discoverHistory(t, h, token, record, "", 21)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	code, _ = discoverHistory(t, h, token, record, "bad\x00cursor", 1)
	require.Equal(t, http.StatusBadRequest, code)
	for i := range legacyimport.MaxRegistrationFields {
		user["extra-"+strconv.Itoa(i)] = false
	}
	raw, digest = historyBundle(t, "discovery-wide", map[string][]map[string]any{"users": {user}})
	args = saveHistoryBundle(t, h.store, scope, raw, digest)
	r = prepareHistory(t, h.store, scope, args)
	_, err = h.store.ApplyHistory(t.Context(), scope, r, func(context.Context) error { return nil })
	require.NoError(t, err)
	page, err = h.store.OwnerHistory(t.Context(), scope, uuid.Nil, 10)
	require.NoError(t, err)
	wideRecord := uuid.Nil
	for _, item := range page.Items {
		if item.SourceInstance == "discovery-wide" {
			wideRecord = item.ID
		}
	}
	require.NotEqual(t, uuid.Nil, wideRecord)
	code, body = discoverHistory(t, h, token, wideRecord, "", 1)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(body))
	require.NotContains(t, string(body), `"items"`)
}
