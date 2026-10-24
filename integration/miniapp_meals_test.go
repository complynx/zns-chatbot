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

func mealsAppFixture(t *testing.T) (paymentFixture, *peers, appSession, db.MealDraftChange) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, _, change := mealFixtureWithPeers(t, p)
	session := detailsAppSession(t, f.H, 11002, 111)
	require.Equal(t, f.OwnerID, session.UserID)
	return f, p, session, change
}

func mealsAppBody(t *testing.T, change db.MealDraftChange) string {
	t.Helper()
	raw, err := json.Marshal(
		map[string]any{
			"operation_id":             change.OperationID,
			"expected_event_version":   change.ExpectedEventVersion,
			"expected_catalog_version": change.ExpectedCatalogVersion,
			"expected_draft_version":   change.ExpectedDraftVersion,
			"choices":                  change.Choices,
		},
	)
	require.NoError(t, err)
	return string(raw)
}

func mealsAppPath(event uuid.UUID) string { return "/app/meals/" + event.String() }

func mealsAppDraft(t *testing.T, raw []byte) db.MealDraft {
	t.Helper()
	var result db.MealDraft
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func TestMiniAppMealDraftRestartAndAgentContinuation(t *testing.T) {
	t.Parallel()
	f, p, session, a := mealsAppFixture(t)
	path := mealsAppPath(a.EventID)
	code, raw, headers := appCall(t, f.H, http.MethodGet, path+"/catalog", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.Contains(t, string(raw), "Europe/Minsk")
	body := mealsAppBody(t, a)
	code, first, _ := appCall(t, f.H, http.MethodPost, path+"/draft", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(first))
	require.Equal(t, int64(1), mealsAppDraft(t, first).Version)
	f.H.stop(t)
	f.H.start(t)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/draft", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	agent := a
	agent.OperationID = uuid.New()
	agent.ExpectedDraftVersion = 1
	agent = completeMeal(agent)
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, "/internal/agent/meals/save_draft", map[string]any{
		"protocol_version": 1,
		"operation_id":     agent.OperationID,
		"idempotency_key":  agent.OperationID,
		"arguments": map[string]any{
			"event_id":                 agent.EventID,
			"expected_event_version":   agent.ExpectedEventVersion,
			"expected_catalog_version": agent.ExpectedCatalogVersion,
			"expected_draft_version":   agent.ExpectedDraftVersion,
			"choices":                  agent.Choices,
		},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"succeeded"`)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/draft", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(2), mealsAppDraft(t, raw).Version)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/draft/operations/"+a.OperationID.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	clearChange := a
	clearChange.OperationID = uuid.New()
	clearChange.ExpectedDraftVersion = 2
	clearChange.Choices = []orders.MealDayChoice{}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/draft",
		mealsAppBody(t, clearChange),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(3), mealsAppDraft(t, raw).Version)
	require.True(t, mealsAppDraft(t, raw).Cleared)
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
			Text: "Continue after clearing the saved meal draft",
		},
	}
	input, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := f.H.service.Accept(t.Context(), update, input)
	require.NoError(t, err)
	f.H.await(t, accepted.ID, "completed")
	scope := ownerProfileScope(t, f.H, session.UserID)
	actions, err := f.H.store.RecentMiniAppMealDraftActions(t.Context(), scope, accepted.ID)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	require.Equal(t, int64(3), actions[0].DraftVersion)
	require.True(t, actions[0].Cleared)
	require.Equal(t, int64(1), actions[1].DraftVersion)
	p.mu.Lock()
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, "Recent completed Mini App meal draft requests")
	require.Contains(t, prompt, a.OperationID.String())
	require.NotContains(t, prompt, session.Token)
	require.NotContains(t, prompt, `"known_amount_minor"`)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", a.EventID)
	require.NoError(t, err)
	actions, err = f.H.store.RecentMiniAppMealDraftActions(t.Context(), scope, accepted.ID)
	require.NoError(t, err)
	require.Empty(t, actions, "Current event access also filters manual context")
}

func TestMiniAppMealDraftRejectsForeignStaleInvalidAndLoggedOutRequests(t *testing.T) {
	t.Parallel()
	f, _, session, a := mealsAppFixture(t)
	path := mealsAppPath(a.EventID)
	other := detailsAppSession(t, f.H, 11003, 112)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/draft",
		mealsAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	for _, suffix := range []string{"/catalog", "/draft", "/draft/operations/" + a.OperationID.String()} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path+suffix, "", other.Token, "")
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/draft", mealsAppBody(t, a), other.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/draft",
		mealsAppBody(t, a),
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
		path+"/draft",
		mealsAppBody(t, stale),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	for _, invalid := range []string{`{"operation_id":"` + uuid.NewString() + `","expected_event_version":1,"expected_catalog_version":1,"choices":[]}`, `{"owner_id":"` + uuid.NewString() + `"}`, mealsAppBody(t, a) + ` {}`, `null`} {
		code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/draft", invalid, session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, _, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/draft",
		mealsAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/draft", "", session.Token, "")
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}

func TestMiniAppMealDraftJournalFailureRollsBackEverything(t *testing.T) {
	t.Parallel()
	f, _, session, a := mealsAppFixture(t)
	path := mealsAppPath(a.EventID)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION reject_selection_action() RETURNS trigger LANGUAGE plpgs" +
"ql AS $$ BEGIN RAISE EXCEPTION 'journal unavailable'; END $$; CREATE TRI" +
"GGER reject_selection_action BEFORE INSERT ON shuttle_miniapp_meal_draft" +
"_actions FOR EACH ROW EXECUTE FUNCTION reject_selection_action()")
	require.NoError(t, err)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/draft",
		mealsAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	scope := ownerProfileScope(t, f.H, session.UserID)
	current, err := f.H.store.OwnMealDraft(t.Context(), scope, a.EventID)
	require.NoError(t, err)
	require.Zero(t, current.Version)
	_, err = f.H.store.OwnMealDraftOperation(t.Context(), scope, a.EventID, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER reject_selection_action ON shuttle_miniapp_meal_draft_actions")
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/draft",
		mealsAppBody(t, a),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(1), mealsAppDraft(t, raw).Version)
}

func TestMiniAppMealDraftRechecksSessionAfterEventLockWait(t *testing.T) {
	t.Parallel()
	f, _, session, a := mealsAppFixture(t)
	blocking, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(t.Context()) }()
	_, err = blocking.Exec(t.Context(), "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", a.EventID)
	require.NoError(t, err)
	var blockingPID int
	require.NoError(t, blocking.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&blockingPID))
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	body := mealsAppBody(t, a)
	go func() {
		code, raw, _, requestErr := appHTTPCall(
			f.H,
			http.MethodPost,
			mealsAppPath(a.EventID)+"/draft",
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
"ocking_pids(pid)))", blockingPID).
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
	current, err := f.H.store.OwnMealDraft(t.Context(), ownerProfileScope(t, f.H, session.UserID), a.EventID)
	require.NoError(t, err)
	require.Zero(t, current.Version)
}

func TestMiniAppMealsEventsKeepPastAssignedUnpaidHistory(t *testing.T) {
	t.Parallel()
	f, _, session, a := mealsAppFixture(t)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET starts_at=clock_timestamp()-interval '2 days'," +
"ends_at=clock_timestamp()-interval '1 day' WHERE id=$1", a.EventID)
	require.NoError(t, err)
	code, raw, _ := appCall(t, f.H, http.MethodGet, "/app/meals/events", "", session.Token, "")
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
	require.Empty(t, agentResult.Result.Events, "A meals entitlement does not enable service orders")
	other := detailsAppSession(t, f.H, 11003, 113)
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/meals/events", "", other.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Empty(t, list.Events)
}

func TestMiniAppMealsReadSuppressesSessionExpiredWhileCatalogBlocked(t *testing.T) {
	t.Parallel()
	f, _, session, a := mealsAppFixture(t)
	blocking, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(t.Context()) }()
	_, err = blocking.Exec(t.Context(), "LOCK TABLE shuttle_meal_catalogs IN ACCESS EXCLUSIVE MODE")
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
			mealsAppPath(a.EventID)+"/catalog",
			"",
			session.Token,
			"",
		)
		done <- response{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		probeErr := f.H.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='shuttle_meal_catalo" +
"gs'::regclass AND NOT granted)").
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

func TestMiniAppMealsCatalogHistoryAndVersionValidation(t *testing.T) {
	t.Parallel()
	f, _, session, a := mealsAppFixture(t)
	catalog, err := f.H.store.MealCatalogConfiguration(t.Context(), ownerProfileScope(t, f.H, f.AdminID), a.EventID, 1)
	require.NoError(t, err)
	catalog.Document.Days[0].Dinner.Items[0].PriceMinor = 99
	publishMealCatalog(t, f, 1, catalog.Document)
	path := mealsAppPath(a.EventID) + "/catalog"
	for _, sample := range []struct {
		query   string
		price   int64
		version int64
	}{{"", 99, 2}, {"?version=0", 99, 2}, {"?version=1", 50, 1}} {
		code, raw, _ := appCall(t, f.H, http.MethodGet, path+sample.query, "", session.Token, "")
		require.Equal(t, http.StatusOK, code, string(raw))
		var view db.MealCatalogView
		require.NoError(t, json.Unmarshal(raw, &view))
		require.Equal(t, sample.version, view.Version)
		require.Equal(t, sample.price, view.Document.Days[0].Dinner.Items[0].PriceMinor)
	}
	for _, version := range []string{"-1", "word", "9223372036854775808"} {
		code, raw, _ := appCall(t, f.H, http.MethodGet, path+"?version="+version, "", session.Token, "")
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, raw, _ := appCall(t, f.H, http.MethodGet, path+"?version=3", "", session.Token, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))
}
