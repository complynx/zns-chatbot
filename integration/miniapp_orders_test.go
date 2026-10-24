package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/orders"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func ordersAppFixture(t *testing.T) (paymentFixture, *peers, appSession, db.OrdersSelectionChange) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, _, change := selectionFixtureWithPeers(t, p)
	session := detailsAppSession(t, f.H, 11002, 111)
	require.Equal(t, f.OwnerID, session.UserID)
	return f, p, session, change
}

func ordersAppBody(t *testing.T, change db.OrdersSelectionChange) string {
	t.Helper()
	raw, err := json.Marshal(
		map[string]any{
			"operation_id":             change.OperationID,
			"expected_event_version":   change.ExpectedEventVersion,
			"expected_catalog_version": change.ExpectedCatalogVersion,
			"expected_draft_version":   change.ExpectedDraftVersion,
			"selections":               change.Selections,
		},
	)
	require.NoError(t, err)
	return string(raw)
}

func ordersAppPath(event uuid.UUID) string { return "/app/orders/" + event.String() }

func ordersAppSelection(t *testing.T, raw []byte) db.OrdersSelection {
	t.Helper()
	var result db.OrdersSelection
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func TestMiniAppOrdersSelectionRestartAndAgentContinuation(t *testing.T) {
	t.Parallel()
	f, p, session, a := ordersAppFixture(t)
	path := ordersAppPath(a.EventID)
	code, raw, headers := appCall(t, f.H, http.MethodGet, path+"/catalog", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.Contains(t, string(raw), "Europe/Minsk")
	quoteBody, err := json.Marshal(
		map[string]any{
			"expected_event_version":   a.ExpectedEventVersion,
			"expected_catalog_version": a.ExpectedCatalogVersion,
			"selections":               a.Selections,
		},
	)
	require.NoError(t, err)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/quote", string(quoteBody), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var quote db.OrdersQuoteView
	require.NoError(t, json.Unmarshal(raw, &quote))
	require.Equal(t, int64(2568), quote.TotalMinor)
	body := ordersAppBody(t, a)
	code, first, _ := appCall(t, f.H, http.MethodPost, path+"/selection", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(first))
	require.Equal(t, int64(1), ordersAppSelection(t, first).Version)
	f.H.stop(t)
	f.H.start(t)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/selection", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	agent := a
	agent.OperationID = uuid.New()
	agent.ExpectedDraftVersion = 1
	agent.Selections = []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 1}}
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, "/internal/agent/orders/save_selection", map[string]any{
		"protocol_version": 1,
		"operation_id":     agent.OperationID,
		"idempotency_key":  agent.OperationID,
		"arguments": map[string]any{
			"event_id":                 agent.EventID,
			"expected_event_version":   agent.ExpectedEventVersion,
			"expected_catalog_version": agent.ExpectedCatalogVersion,
			"expected_draft_version":   agent.ExpectedDraftVersion,
			"selections":               agent.Selections,
		},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"succeeded"`)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/selection", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(2), ordersAppSelection(t, raw).Version)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/selection/operations/"+a.OperationID.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	clearChange := a
	clearChange.OperationID = uuid.New()
	clearChange.ExpectedDraftVersion = 2
	clearChange.Selections = []orders.Selection{}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, clearChange),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(3), ordersAppSelection(t, raw).Version)
	require.Nil(t, ordersAppSelection(t, raw).Quote)
	p.mu.Lock()
	require.Empty(t, p.runs)
	require.Empty(t, p.sent)
	p.mu.Unlock()
	update := telegram.Update{
		ID: 99101,
		Message: &telegram.Message{
			ID:   99101,
			From: &telegram.User{ID: 11002},
			Chat: telegram.Chat{ID: 11002, Type: "private"},
			Text: "Continue after clearing the saved selection",
		},
	}
	input, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := f.H.service.Accept(t.Context(), update, input)
	require.NoError(t, err)
	f.H.await(t, accepted.ID, "completed")
	scope := ownerProfileScope(t, f.H, session.UserID)
	actions, err := f.H.store.RecentMiniAppOrdersSelectionActions(t.Context(), scope, accepted.ID)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	require.Equal(t, int64(3), actions[0].DraftVersion)
	require.True(t, actions[0].Cleared)
	require.Equal(t, int64(1), actions[1].DraftVersion)
	p.mu.Lock()
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, "Recent completed Mini App order selection requests")
	require.Contains(t, prompt, a.OperationID.String())
	require.NotContains(t, prompt, session.Token)
	require.NotContains(t, prompt, `"total_minor":2568`)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", a.EventID)
	require.NoError(t, err)
	actions, err = f.H.store.RecentMiniAppOrdersSelectionActions(t.Context(), scope, accepted.ID)
	require.NoError(t, err)
	require.Empty(t, actions, "Current event access also filters manual context")
}

func TestMiniAppOrdersSelectionRejectsForeignStaleInvalidAndLoggedOutRequests(t *testing.T) {
	t.Parallel()
	f, _, session, a := ordersAppFixture(t)
	path := ordersAppPath(a.EventID)
	other := detailsAppSession(t, f.H, 11003, 112)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	for _, suffix := range []string{"/catalog", "/selection", "/selection/operations/" + a.OperationID.String()} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path+suffix, "", other.Token, "")
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/selection", ordersAppBody(t, a), other.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, a),
		session.Token,
		"https://foreign.example.invalid",
	)
	require.Equal(t, http.StatusForbidden, code)
	stale := a
	stale.OperationID = uuid.New()
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, stale),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	for _, invalid := range []string{`{"operation_id":"` + uuid.NewString() + "\",\"expected_event_version\":1,\"expected_catalog_version\":1,\"selections\":[" +
"]}", `{"owner_id":"` + uuid.NewString() + `"}`, ordersAppBody(t, a) + ` {}`, `null`} {
		code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/selection", invalid, session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, _, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/selection", "", session.Token, "")
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}

func TestMiniAppOrdersSelectionJournalFailureRollsBackEverything(t *testing.T) {
	t.Parallel()
	f, _, session, a := ordersAppFixture(t)
	path := ordersAppPath(a.EventID)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION reject_selection_action() RETURNS trigger LANGUAGE plpgs" +
"ql AS $$ BEGIN RAISE EXCEPTION 'journal unavailable'; END $$; CREATE TRI" +
"GGER reject_selection_action BEFORE INSERT ON shuttle_miniapp_orders_sel" +
"ection_actions FOR EACH ROW EXECUTE FUNCTION reject_selection_action()")
	require.NoError(t, err)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	scope := ownerProfileScope(t, f.H, session.UserID)
	current, err := f.H.store.OwnOrdersSelection(t.Context(), scope, a.EventID)
	require.NoError(t, err)
	require.Zero(t, current.Version)
	_, err = f.H.store.OwnOrdersSelectionOperation(t.Context(), scope, a.EventID, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER reject_selection_action ON shuttle_miniapp_orders_selection_actions")
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/selection",
		ordersAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(1), ordersAppSelection(t, raw).Version)
}

func TestMiniAppOrdersSelectionRechecksSessionAfterEventLockWait(t *testing.T) {
	t.Parallel()
	f, _, session, a := ordersAppFixture(t)
	blocking, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(t.Context()) }()
	_, err = blocking.Exec(t.Context(), "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", a.EventID)
	require.NoError(t, err)
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	body := ordersAppBody(t, a)
	go func() {
		code, raw, _, requestErr := appHTTPCall(
			f.H,
			http.MethodPost,
			ordersAppPath(a.EventID)+"/selection",
			body,
			session.Token,
			appTestOrigin,
		)
		done <- response{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		probeErr := f.H.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_bl" +
"ocking_pids(pid)))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return probeErr == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	_, err = blocking.Exec(
		t.Context(),
		"UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1",
		session.UserID,
	)
	require.NoError(t, err)
	require.NoError(t, blocking.Commit(t.Context()))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
	case <-time.After(5 * time.Second):
		t.Fatal("Blocked save did not finish")
	}
	current, err := f.H.store.OwnOrdersSelection(t.Context(), ownerProfileScope(t, f.H, session.UserID), a.EventID)
	require.NoError(t, err)
	require.Zero(t, current.Version)
}

func TestMiniAppOrdersSelectionConcurrentAgentSaveAndReplayDoNotForgeManualHistory(t *testing.T) {
	t.Parallel()
	f, _, session, a := ordersAppFixture(t)
	scope := ownerProfileScope(t, f.H, session.UserID)
	agent := a
	agent.OperationID = uuid.New()
	agentDone := make(chan error, 1)
	go func() { _, err := f.H.store.SaveOwnOrdersSelection(t.Context(), scope, agent); agentDone <- err }()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		ordersAppPath(a.EventID)+"/selection",
		ordersAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	agentErr := <-agentDone
	switch code {
	case http.StatusOK:
		require.ErrorIs(t, agentErr, db.ErrConflict)
	case http.StatusConflict:
		require.NoError(t, agentErr)
	default:
		t.Fatalf("Unexpected form result %d: %s", code, raw)
	}
	current, err := f.H.store.OwnOrdersSelection(t.Context(), scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	next := a
	next.OperationID = uuid.New()
	next.ExpectedDraftVersion = 1
	_, err = f.H.store.SaveOwnOrdersSelection(t.Context(), scope, next)
	require.NoError(t, err)
	var before, after int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_miniapp_orders_selection_actions").
			Scan(&before),
	)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		ordersAppPath(a.EventID)+"/selection",
		ordersAppBody(t, next),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_miniapp_orders_selection_actions").
			Scan(&after),
	)
	require.Equal(t, before, after, "Reading/replaying an agent receipt cannot create a new manual action")
}

func TestMiniAppOrdersEventsKeepPastAssignedUnpaidHistory(t *testing.T) {
	t.Parallel()
	f, _, session, a := ordersAppFixture(t)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET starts_at=clock_timestamp()-interval '2 days'," +
"ends_at=clock_timestamp()-interval '1 day' WHERE id=$1", a.EventID)
	require.NoError(t, err)
	code, raw, _ := appCall(t, f.H, http.MethodGet, "/app/orders/events", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var list events.ContextList
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list.Events, 1)
	require.Equal(t, a.EventID, list.Events[0].ID)
	require.Equal(t, "Europe/Minsk", list.Events[0].Timezone)
	code, agentRaw := adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		"/internal/agent/orders/events",
		map[string]any{"protocol_version": 1, "operation_id": "past-events", "arguments": map[string]any{}},
	)
	require.Equal(t, http.StatusOK, code, string(agentRaw))
	var agentResult struct {
		Result events.ContextList `json:"result"`
	}
	require.NoError(t, json.Unmarshal(agentRaw, &agentResult))
	require.Equal(t, list, agentResult.Result, "Agent and form list the same past entitled event")
	other := detailsAppSession(t, f.H, 11003, 113)
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/orders/events", "", other.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Empty(t, list.Events)
}

func TestMiniAppOrdersReadSuppressesSessionExpiredWhileCatalogBlocked(t *testing.T) {
	t.Parallel()
	f, _, session, a := ordersAppFixture(t)
	blocking, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(t.Context()) }()
	_, err = blocking.Exec(t.Context(), "LOCK TABLE shuttle_orders_catalogs IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	go func() {
		code, raw, _, requestErr := appHTTPCall(
			f.H,
			http.MethodGet,
			ordersAppPath(a.EventID)+"/catalog",
			"",
			session.Token,
			"",
		)
		done <- response{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		probeErr := f.H.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='shuttle_orders_cata" +
"logs'::regclass AND NOT granted)").
			Scan(&waiting)
		return probeErr == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	_, err = blocking.Exec(
		t.Context(),
		"UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1",
		session.UserID,
	)
	require.NoError(t, err)
	require.NoError(t, blocking.Commit(t.Context()))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
		require.NotContains(t, string(result.raw), "soup")
	case <-time.After(5 * time.Second):
		t.Fatal("Blocked catalog read did not finish")
	}
}
