package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

const appDetailsPath = "/app/registration-details"

func detailsApp(t *testing.T) (*harness, *peers, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 91)
	h := setup(t, p)
	return h, p, detailsAppSession(t, h, 456, 91)
}

func detailsAppSession(t *testing.T, h *harness, user int64, nonce byte) appSession {
	t.Helper()
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(user), nonce), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	return session
}

func appDetailsBody(t *testing.T, operation uuid.UUID, version int64, value string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"operation_id": operation, "expected_version": version, "value": value})
	require.NoError(t, err)
	return string(raw)
}

func appDetailsStatus(t *testing.T, raw []byte) registrationdetails.Status {
	t.Helper()
	var status registrationdetails.Status
	require.NoError(t, json.Unmarshal(raw, &status))
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.Len(t, fields, 3, "Only presence and version may leave the protected store")
	return status
}

func TestMiniAppDetailsContinueAgentRequestAndRecoverHistoricalReceipt(t *testing.T) {
	t.Parallel()
	h, p, session := detailsApp(t)
	scope := ownerProfileScope(t, h, session.UserID)
	_, admin := registrationUser(t, h, 95501, true)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	pending := beginDetailsRequest(t, h, scope, event, uuid.New())
	operation := uuid.New()
	body := appDetailsBody(t, operation, 0, "Synthetic Legal MiniApp Name")
	code, first, headers := appCall(
		t,
		h,
		http.MethodPost,
		appDetailsPath+"/legal-name",
		body,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(first))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.Equal(t, registrationdetails.Status{Version: 1, LegalNamePresent: true}, appDetailsStatus(t, first))
	h.stop(t)
	h.start(t)
	code, raw, _ := appCall(t, h, http.MethodPost, appDetailsPath+"/legal-name", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	// The agent's existing API/store path can finish the same form-started record.
	saved, err := h.store.MutateRegistrationDetails(
		t.Context(),
		scope,
		registrationdetails.Mutation{
			OperationID:     uuid.New(),
			ExpectedVersion: 1,
			Field:           registrationdetails.PassportNumber,
			Value:           "SYNTHETIC-APP-PASSPORT",
		},
		p.registrationDetails,
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, saved.Version)
	ready, err := h.store.DetailsRequest(t.Context(), scope, pending.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", ready.State)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodGet,
		appDetailsPath+"/operations/"+operation.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw), "Receipt remains historical after a later agent write")
	code, raw, _ = appCall(t, h, http.MethodGet, appDetailsPath, "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, saved, appDetailsStatus(t, raw))
	p.mu.Lock()
	require.Empty(t, p.runs)
	require.Empty(t, p.sent)
	p.mu.Unlock()
	taskID := h.submitText(t, 89801, "Continue after form entry")
	h.await(t, taskID, "completed")
	actions, err := h.store.RecentMiniAppDetailsActions(t.Context(), scope, taskID)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, operation, actions[0].OperationID)
	require.EqualValues(t, 1, actions[0].Status.Version)
	p.mu.Lock()
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, "Recent completed Mini App registration details requests")
	require.NotContains(t, prompt, "Synthetic Legal MiniApp Name")
	require.NotContains(t, prompt, "SYNTHETIC-APP-PASSPORT")
	require.NotContains(t, prompt, session.Token)
	profile, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Empty(t, profile.FullName, "Legal name is not the public/display name")
	var registrations int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registrations WHERE user_id=$1", session.UserID).
			Scan(&registrations),
	)
	require.Zero(t, registrations, "Completing fields is not registration consent")
}

func TestMiniAppDetailsRejectForeignStaleInvalidAndLoggedOutRequests(t *testing.T) {
	t.Parallel()
	h, _, session := detailsApp(t)
	other := detailsAppSession(t, h, 11002, 92)
	operation := uuid.New()
	original := appDetailsBody(t, operation, 0, "Synthetic Legal MiniApp Name")
	code, raw, _ := appCall(t, h, http.MethodPost, appDetailsPath+"/legal-name", original, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(t, h, http.MethodGet, appDetailsPath+"/operations/"+operation.String(), "", other.Token, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))
	for _, body := range []string{appDetailsBody(t, operation, 0, "Different private value"), appDetailsBody(t, uuid.New(), 0, "SYNTHETIC-STALE-PASSPORT")} {
		code, raw, _ = appCall(
			t,
			h,
			http.MethodPost,
			appDetailsPath+"/passport-number",
			body,
			session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusConflict, code, string(raw))
		require.NotContains(t, string(raw), "SYNTHETIC")
	}
	for _, body := range []string{`{"operation_id":"` + uuid.NewString() + `","value":"PRIVATE-NO-VERSION"}`, `{"operation_id":"` + uuid.NewString() + `","expected_version":1,"value":"PRIVATE-UNKNOWN","user_id":"` + other.UserID.String() + `"}`, appDetailsBody(t, uuid.New(), 1, "PRIVATE\nINVALID")} {
		code, raw, _ = appCall(t, h, http.MethodPost, appDetailsPath+"/legal-name", body, session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
		require.NotContains(t, string(raw), "PRIVATE")
	}
	code, _, _ = appCall(
		t,
		h,
		http.MethodPost,
		appDetailsPath+"/legal-name",
		original,
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code)
	code, _, _ = appCall(t, h, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code)
	code, _, _ = appCall(t, h, http.MethodPost, appDetailsPath+"/legal-name", original, session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code)
	code, _, _ = appCall(t, h, http.MethodGet, appDetailsPath+"/operations/"+operation.String(), "", session.Token, "")
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestMiniAppDetailsJournalFailureRollsBackPrivateWriteReceiptAndExpectation(t *testing.T) {
	t.Parallel()
	h, p, session := detailsApp(t)
	scope := ownerProfileScope(t, h, session.UserID)
	_, admin := registrationUser(t, h, 95502, true)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	pending := beginDetailsRequest(t, h, scope, event, uuid.New())
	_, err := h.store.MutateRegistrationDetails(
		t.Context(),
		scope,
		registrationdetails.Mutation{
			OperationID:     uuid.New(),
			ExpectedVersion: 0,
			Field:           registrationdetails.LegalName,
			Value:           "Synthetic Legal Atomic Name",
		},
		p.registrationDetails,
	)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(t.Context(), `CREATE FUNCTION reject_private_action() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected journal failure'; END $$;
CREATE TRIGGER reject_private_action BEFORE INSERT ON shuttle_miniapp_details_actions FOR EACH ROW EXECUTE FUNCTION reject_private_action()`)
	require.NoError(t, err)
	operation := uuid.New()
	body := appDetailsBody(t, operation, 1, "SYNTHETIC-ATOMIC-PASSPORT")
	code, raw, _ := appCall(
		t,
		h,
		http.MethodPost,
		appDetailsPath+"/passport-number",
		body,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	require.NotContains(t, string(raw), "SYNTHETIC")
	status, err := h.store.RegistrationDetailsStatus(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, registrationdetails.Status{Version: 1, LegalNamePresent: true}, status)
	_, err = h.store.RegistrationDetailsOperation(t.Context(), scope, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	current, err := h.store.DetailsRequest(t.Context(), scope, pending.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", current.State)
	_, err = h.store.Pool().Exec(t.Context(), "DROP TRIGGER reject_private_action ON shuttle_miniapp_details_actions")
	require.NoError(t, err)
	code, raw, _ = appCall(t, h, http.MethodPost, appDetailsPath+"/passport-number", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	current, err = h.store.DetailsRequest(t.Context(), scope, pending.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.State)
}

func TestMiniAppDetailsRecheckSessionAfterOwnerLockWait(t *testing.T) {
	t.Parallel()
	h, _, session := detailsApp(t)
	blocking, err := h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(t.Context()) }()
	_, err = blocking.Exec(t.Context(), "SELECT id FROM shuttle_users WHERE id=$1 FOR UPDATE", session.UserID)
	require.NoError(t, err)
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	body := appDetailsBody(t, uuid.New(), 0, "Synthetic Blocked Name")
	go func() {
		code, raw, _, requestErr := appHTTPCall(
			h,
			http.MethodPost,
			appDetailsPath+"/legal-name",
			body,
			session.Token,
			appTestOrigin,
		)
		done <- response{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err = h.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return err == nil && waiting
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
		t.Fatal("Blocked form write did not finish")
	}
	status, err := h.store.RegistrationDetailsStatus(t.Context(), ownerProfileScope(t, h, session.UserID))
	require.NoError(t, err)
	require.Zero(t, status.Version)
}

func TestMiniAppDetailsRemainUnavailableWithoutProtectionKey(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	for _, path := range []string{appDetailsPath, appDetailsPath + "/operations/" + uuid.NewString()} {
		code, raw, _ := appCall(t, h, http.MethodGet, path, "", "", "")
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	code, raw, _ := appCall(
		t,
		h,
		http.MethodPost,
		appDetailsPath+"/legal-name",
		`{"value":"PRIVATE-DISABLED"}`,
		"",
		appTestOrigin,
	)
	require.Equal(t, http.StatusMethodNotAllowed, code, string(raw))
	require.NotContains(t, string(raw), "PRIVATE-DISABLED")
}
