package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMiniAppProfileCompletesAgentExpectationAndRecoversExactWrite(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(456), 21), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	scope := ownerProfileScope(t, h, session.UserID)
	pending := changeProfile(t, h, scope, profileChange("request_name", 0))
	require.NotNil(t, pending.NameExpectation)
	input := map[string]any{"operation_id": uuid.New(), "expected_version": pending.Version, "full_name": "  Анна QA  "}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	code, first, _ := appCall(t, h, http.MethodPost, "/app/profile/name", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(first))
	var saved db.UserProfile
	require.NoError(t, json.Unmarshal(first, &saved))
	require.Equal(t, "Анна QA", saved.FullName)
	require.Nil(t, saved.NameExpectation)
	require.Equal(t, pending.Version+1, saved.Version)
	h.stop(t)
	h.start(t)
	code, replay, _ := appCall(t, h, http.MethodPost, "/app/profile/name", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(replay))
	require.JSONEq(t, string(first), string(replay))
	input["operation_id"] = uuid.New()
	input["full_name"] = "Stale overwrite"
	body, err = json.Marshal(input)
	require.NoError(t, err)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/name", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusConflict, code, string(raw))
	language := map[string]any{"operation_id": uuid.New(), "expected_version": saved.Version, "bot_language": "ru"}
	body, err = json.Marshal(language)
	require.NoError(t, err)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/language", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	current, err := h.store.GetProfile(context.Background(), scope)
	require.NoError(t, err)
	require.Equal(t, "ru", current.BotLanguage)
	require.Equal(t, "Анна QA", current.FullName)
	require.Nil(t, current.NameExpectation)
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/profile", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), session.Token)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/profile/name",
		`{"operation_id":"`+uuid.NewString()+`","expected_version":0,"full_name":"X","user_id":"`+uuid.NewString()+`"}`,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, _, _ = appCall(t, h, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/language", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Empty(t, p.sent)
}

func TestMiniAppProfileSuppressesDataWhenSessionExpiresDuringRead(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(11002), 9), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	scope := ownerProfileScope(t, h, session.UserID)
	change := profileChange("set_name", 0)
	change.FullName = "Sensitive QA name"
	changeProfile(t, h, scope, change)
	ctx := context.Background()
	blocking, err := h.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(ctx) }()
	_, err = blocking.Exec(ctx, "LOCK TABLE shuttle_user_profiles IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	type reply struct {
		code int
		raw  []byte
		err  error
	}
	finished := make(chan reply, 1)
	go func() {
		status, result, _, requestErr := appHTTPCall(
			h,
			http.MethodGet,
			"/app/profile",
			"",
			session.Token,
			appTestOrigin,
		)
		finished <- reply{status, result, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := h.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks
WHERE relation='shuttle_user_profiles'::regclass AND NOT granted)`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1", session.UserID)
	require.NoError(t, err)
	require.NoError(t, blocking.Commit(ctx))
	select {
	case result := <-finished:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
		require.NotContains(t, string(result.raw), "Sensitive QA name")
	case <-time.After(5 * time.Second):
		t.Fatal("Profile read did not finish after releasing its DB wait")
	}
}

func TestMiniAppProfileActionsReachNextAgentOnceAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(456), 71), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	operation := uuid.New()
	body := `{"operation_id":"` + operation.String() + `","expected_version":0,"full_name":"Mini App QA"}`
	code, first, _ := appCall(t, h, http.MethodPost, "/app/profile/name", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(first))
	h.stop(t)
	h.start(t)
	code, replay, _ := appCall(t, h, http.MethodPost, "/app/profile/name", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(replay))
	require.JSONEq(t, string(first), string(replay))
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/profile/language",
		`{"operation_id":"`+uuid.NewString()+`","expected_version":0,"bot_language":"ru"}`,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	p.mu.Lock()
	require.Empty(t, p.runs, "Form saves must not start a proactive agent run")
	require.Empty(t, p.sent)
	p.mu.Unlock()
	taskID := h.submitText(t, 89700, "What did I change in the form?")
	h.await(t, taskID, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	prompt := p.createdInputs[0]
	require.Contains(t, prompt, "Recent completed Mini App profile requests")
	require.Contains(t, prompt, `"kind":"set_name"`)
	require.Contains(t, prompt, `"profile_version":1`)
	require.Equal(t, 1, strings.Count(prompt, operation.String()), "A retry must not duplicate the historical action")
	require.NotContains(t, prompt, `"kind":"set_language"`, "Rejected writes must not appear as completed actions")
	require.NotContains(t, prompt, session.Token)
}

func TestMiniAppProfileJournalFailureRollsBackWrite(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(456), 72), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	scope := ownerProfileScope(t, h, session.UserID)
	operation := uuid.New()
	_, err := h.store.Pool().
		Exec(t.Context(), `CREATE FUNCTION reject_manual_action() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected journal failure'; END $$;
CREATE TRIGGER reject_manual_action BEFORE INSERT ON shuttle_miniapp_profile_actions FOR EACH ROW EXECUTE FUNCTION reject_manual_action()`)
	require.NoError(t, err)
	body := `{"operation_id":"` + operation.String() + `","expected_version":0,"full_name":"Atomic QA"}`
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/name", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	profile, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Zero(t, profile.Version)
	require.Empty(t, profile.FullName)
	_, err = h.store.ProfileOperation(t.Context(), scope, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.Pool().Exec(t.Context(), "DROP TRIGGER reject_manual_action ON shuttle_miniapp_profile_actions")
	require.NoError(t, err)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/name", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	taskID := h.submitText(t, 89701, "Continue after recovered form save")
	h.await(t, taskID, "completed")
	actions, err := h.store.RecentMiniAppProfileActions(t.Context(), scope, taskID)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, operation, actions[0].OperationID)
	require.EqualValues(t, 1, actions[0].ProfileVersion)
}

func TestMiniAppProfileContextIsBoundedAndOwnerScoped(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	bootstrap := func(user int64, nonce byte) appSession {
		code, raw, _ := appCall(
			t,
			h,
			http.MethodPost,
			appBootstrapPath,
			appBody(appProof(user), nonce),
			"",
			appTestOrigin,
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		var session appSession
		require.NoError(t, json.Unmarshal(raw, &session))
		return session
	}
	own := bootstrap(456, 73)
	other := bootstrap(11002, 74)
	foreignOperation := uuid.New()
	code, raw, _ := appCall(
		t,
		h,
		http.MethodPost,
		"/app/profile/name",
		`{"operation_id":"`+foreignOperation.String()+`","expected_version":0,"full_name":"Private other owner"}`,
		other.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	for n := range 8 {
		language := "en"
		if n%2 == 1 {
			language = "ru"
		}
		body, err := json.Marshal(
			map[string]any{"operation_id": uuid.New(), "expected_version": n, "bot_language": language},
		)
		require.NoError(t, err)
		code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/language", string(body), own.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
	}
	taskID := h.submitText(t, 89702, "Continue with current language")
	h.await(t, taskID, "completed")
	scope := ownerProfileScope(t, h, own.UserID)
	actions, err := h.store.RecentMiniAppProfileActions(t.Context(), scope, taskID)
	require.NoError(t, err)
	require.Len(t, actions, 5)
	for n, action := range actions {
		require.Equal(t, "set_language", action.Kind)
		require.EqualValues(t, 8-n, action.ProfileVersion)
		require.NotEqual(t, foreignOperation, action.OperationID)
	}
	otherScope := ownerProfileScope(t, h, other.UserID)
	denied, err := h.store.RecentMiniAppProfileActions(t.Context(), otherScope, taskID)
	require.NoError(t, err)
	require.Empty(t, denied, "A different owner's task cannot disclose history")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotContains(t, p.createdInputs[0], foreignOperation.String())
	require.NotContains(t, p.createdInputs[0], "Private other owner")
}
