package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPassIdentityExportRechecksGlobalIAMRoleBeforeRelease(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 90)

	// Change live introspection without issuing a new token, expiring the session,
	// or notifying the application that its positive IAM cache is stale.
	const apiRoles = "urn:zitadel:iam:org:project:api:roles"
	var downgraded atomic.Bool
	var downgradedReads atomic.Int64
	p.iam.Close()
	p.iam = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/intro" || !downgraded.Load() {
			p.identity(w, r)
			return
		}
		rec := httptest.NewRecorder()
		p.identity(rec, r)
		if rec.Code != http.StatusOK {
			w.WriteHeader(rec.Code)
			return
		}
		var claims map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &claims); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		claims[apiRoles] = json.RawMessage(`{"account.me":{"org":"Org"}}`)
		downgradedReads.Add(1)
		respond(w, claims)
	}))
	t.Cleanup(p.iam.Close)
	h := setup(t, p)
	_, participantToken := registrationUser(t, h, 99002, false)
	operatorID, operatorToken := registrationUser(t, h, 99001, true)
	operator := paymentScope(t, h, operatorID, true)
	session := detailsAppSession(t, h, 99001, 169)
	person := detailsAppSession(t, h, 99002, 170)
	event := identityExportEvent(t, h, operator, operatorToken, registrationdetails.Requirements{LegalName: true})
	const participantName = "Synthetic Protected IAM Participant"
	saveIdentityExportDetails(t, h, person, 3, participantName, "SYNTHETIC-PROTECTED-IAM", "NL")
	assignIdentityExportParticipant(t, h, operatorToken, participantToken, event, registrations.Leader)
	authority, err := h.store.Authority(t.Context(), operator.EventScope, event.ID, operatorID)
	require.NoError(t, err)
	require.Equal(t, db.AuthorityNone, authority.Role)
	_, _, rows := downloadIdentityExport(t, h, session, event.ID)
	require.Len(t, rows, 1)
	require.Equal(t, participantName, rows[0]["legal_name"].Text)

	pool := h.store.Pool()
	blocking, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(context.Background()) }()
	_, err = blocking.Exec(t.Context(), "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", event.ID)
	require.NoError(t, err)
	type response struct {
		status  int
		body    []byte
		headers http.Header
		err     error
	}
	done := make(chan response, 1)
	go func() {
		status, body, headers, requestErr := appHTTPCall(h, http.MethodGet,
			"/app/pass-admin/events/"+event.ID.String()+"/"+identityExportAction, "", session.Token, appTestOrigin)
		done <- response{status, body, headers, requestErr}
	}()
	initialPID := waitForIdentityEventLock(t, h, blocking.Conn().PgConn().PID(), "%LockRegistrationEvent%")

	// Queue ahead of the final event check while the export still waits to read.
	finalBlocker, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = finalBlocker.Rollback(context.Background()) }()
	finalBlockerPID := finalBlocker.Conn().PgConn().PID()
	finalAcquired := make(chan error, 1)
	const finalLockLabel = "identity QA final event blocker"
	go func() {
		_, lockErr := finalBlocker.Exec(t.Context(),
			"SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE /* "+finalLockLabel+" */", event.ID)
		finalAcquired <- lockErr
	}()
	require.Equal(t, finalBlockerPID, waitForIdentityEventLock(t, h, initialPID, "%"+finalLockLabel+"%"))
	require.NoError(t, blocking.Rollback(t.Context()))
	select {
	case lockErr := <-finalAcquired:
		require.NoError(t, lockErr)
	case <-t.Context().Done():
		t.Fatal("identity release blocker did not acquire the event lock")
	case <-time.After(3 * time.Second):
		t.Fatal("identity release blocker did not acquire the event lock")
	}
	waitForIdentityEventLock(t, h, finalBlockerPID, "%LockRegistrationEvent%")

	downgraded.Store(true)
	fresh, err := p.iam.Client().PostForm(p.iam.URL+"/intro", url.Values{"token": {operatorToken}})
	require.NoError(t, err)
	defer fresh.Body.Close()
	require.Equal(t, http.StatusOK, fresh.StatusCode)
	var current struct {
		Active bool                         `json:"active"`
		Roles  map[string]map[string]string `json:"urn:zitadel:iam:org:project:api:roles"`
	}
	require.NoError(t, json.NewDecoder(fresh.Body).Decode(&current))
	require.True(t, current.Active)
	require.Contains(t, current.Roles, "account.me")
	require.NotContains(t, current.Roles, "zinusya.admin")
	code, raw, _ := appCall(t, h, http.MethodGet, "/app/me", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	verifiedReads := downgradedReads.Load()
	require.NoError(t, finalBlocker.Rollback(t.Context()))

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(
			t,
			http.StatusForbidden,
			result.status,
			"download attachment header %q",
			result.headers.Get("Content-Disposition"),
		)
		requireIdentityExportDenied(t, http.StatusForbidden, result.status, result.body, result.headers)
		require.NotContains(t, string(result.body), participantName)
		require.Greater(t, downgradedReads.Load(), verifiedReads)
	case <-time.After(8 * time.Second):
		t.Fatal("identity download did not finish after releasing the event lock")
	}
}

func waitForIdentityEventLock(t *testing.T, h *harness, blocker uint32, queryPattern string) uint32 {
	t.Helper()
	var waitingPID uint32
	require.Eventually(t, func() bool {
		err := h.store.Pool().QueryRow(t.Context(), `
SELECT pid FROM pg_stat_activity
WHERE datname=current_database() AND application_name=current_setting('application_name')
	AND query LIKE $2 AND $1::integer=ANY(pg_blocking_pids(pid))
ORDER BY pid LIMIT 1`, blocker, queryPattern).Scan(&waitingPID)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond, "query %q was not blocked by database PID %d", queryPattern, blocker)
	return waitingPID
}
