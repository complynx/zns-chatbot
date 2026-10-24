package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

func nativeOrderReviewReadFixture(t *testing.T) (paymentFixture, db.PaymentScope, db.OrderPaymentEvidence, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.fileBytes = []byte("%PDF-1.4\nprivate reviewer order proof")
	f, owner, args := orderApprovalFixture(t, p, false)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, f.H.service.Stop(ctx))
	f.H.service = nil
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), owner, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	submitted, err := f.H.store.SubmitOwnOrder(t.Context(), owner, op, args, prep.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	claim, err := f.H.store.SubmitOwnOrderEvidence(
		t.Context(),
		owner,
		uuid.New(),
		orderEvidenceArgs(t, f, submitted.Order, 61801, "native-review-proof"),
	)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	return f, scope, claim, detailsAppSession(t, f.H, 11003, 117)
}

func orderReviewAppPath(event uuid.UUID) string { return "/app/order-reviews/events/" + event.String() }

func TestMiniAppOrderReviewHTTPReadsAgentWorkspaceWithoutRegistrationsModule(t *testing.T) {
	t.Parallel()
	f, scope, claim, session := nativeOrderReviewReadFixture(t)
	op := uuid.New()
	workspace, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		op,
		db.OrderReviewDraftChange{
			Language: "ru",
			Review: db.OrderEvidenceReview{
				EventID:         f.Event.ID,
				EvidenceID:      claim.ID,
				ExpectedVersion: claim.Version,
				Decision:        db.OrderEvidenceRejected,
				Reason:          "Synthetic read-only handoff",
			},
		},
	)
	require.NoError(t, err)
	code, raw, _ := appCall(t, f.H, http.MethodGet, "/app/order-reviews/events", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var choices events.ContextList
	require.NoError(t, json.Unmarshal(raw, &choices))
	require.Len(t, choices.Events, 1)
	require.Equal(t, f.Event.ID, choices.Events[0].ID)
	base := orderReviewAppPath(f.Event.ID)
	code, raw, _ = appCall(t, f.H, http.MethodGet, base, "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var event events.ContextEvent
	require.NoError(t, json.Unmarshal(raw, &event))
	require.Equal(t, f.Event.ID, event.ID)
	code, raw, _ = appCall(t, f.H, http.MethodGet, base+"/evidence", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var queue db.OrderEvidencePage
	require.NoError(t, json.Unmarshal(raw, &queue))
	require.Len(t, queue.Evidence, 1)
	require.Equal(t, claim, queue.Evidence[0])
	code, raw, _ = appCall(t, f.H, http.MethodGet, base+"/evidence/"+claim.ID.String(), "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var detail db.MiniAppOrderReviewDetail
	require.NoError(t, json.Unmarshal(raw, &detail))
	require.Equal(t, claim, detail.Evidence)
	require.Equal(t, claim.OrderID, detail.Order.ID)
	require.Equal(t, claim.UserID, detail.Payer.UserID)
	require.Equal(t, &workspace, detail.Workspace)
	require.Contains(t, detail.Workspace.Preparation.Description, "Плательщик")
	require.NotContains(t, string(raw), "telegram_id")
	require.NotContains(t, string(raw), "passport")
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/order-reviews/operations/"+op.String(), "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt db.OrderReviewReceipt
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "agent", receipt.Source)
	require.Equal(t, &workspace, receipt.Workspace)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		"/app/order-reviews/operations/"+uuid.NewString(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	owner := detailsAppSession(t, f.H, 11002, 118)
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/order-reviews/events", "", owner.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &choices))
	require.Empty(t, choices.Events)
	code, raw, _ = appCall(t, f.H, http.MethodGet, base+"/evidence/"+claim.ID.String(), "", owner.Token, "")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), claim.FileID.String())
	f.H.peers.mu.Lock()
	require.Empty(t, f.H.peers.createAttempts)
	f.H.peers.mu.Unlock()
}

func TestMiniAppOrderReviewHTTPResolvesRevokedPayerAndServesOnlyEvidenceScopedFile(t *testing.T) {
	t.Parallel()
	f, _, claim, session := nativeOrderReviewReadFixture(t)
	_, err := f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	base := orderReviewAppPath(f.Event.ID) + "/evidence/" + claim.ID.String()
	code, raw, _ := appCall(t, f.H, http.MethodGet, base, "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, headers := appCall(t, f.H, http.MethodGet, base+"/content", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "private reviewer order proof")
	require.Equal(t, "attachment", headers.Get("Content-Disposition"))
	require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Contains(t, headers.Get("Content-Security-Policy"), "sandbox")
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		orderReviewAppPath(uuid.New())+"/evidence/"+claim.ID.String()+"/content",
		"",
		session.Token,
		"",
	)
	require.NotEqual(t, http.StatusOK, code)
	require.NotContains(t, string(raw), "private reviewer order proof")
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		orderReviewAppPath(f.Event.ID)+"/evidence/"+claim.FileID.String()+"/content",
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
}

func TestMiniAppOrderReviewContentRechecksSessionAuthorityAndLocatorAfterIO(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "role-removed", "locator-changed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, _, claim, session := nativeOrderReviewReadFixture(t)
			done := make(chan error, 1)
			f.H.peers.mu.Lock()
			f.H.peers.fileReadHook = func() {
				var err error
				switch kind {
				case "logout":
					var code int
					code, _, _, err = appHTTPCall(
						f.H,
						http.MethodPost,
						"/app/session/logout",
						`{}`,
						session.Token,
						appTestOrigin,
					)
					if err == nil && code != http.StatusNoContent {
						err = fmt.Errorf("logout status %d", code)
					}
				case "role-removed":
					_, err = f.H.store.Pool().
						Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
				case "locator-changed":
					_, err = f.H.store.Pool().
						Exec(t.Context(), "UPDATE shuttle_files SET telegram_id='different-review-source' WHERE id=$1", claim.FileID)
				}
				done <- err
			}
			f.H.peers.mu.Unlock()
			code, raw, _ := appCall(
				t,
				f.H,
				http.MethodGet,
				orderReviewAppPath(f.Event.ID)+"/evidence/"+claim.ID.String()+"/content",
				"",
				session.Token,
				"",
			)
			expected := http.StatusForbidden
			if kind == "logout" {
				expected = http.StatusUnauthorized
			}
			if kind == "locator-changed" {
				expected = http.StatusConflict
			}
			require.Equal(t, expected, code, string(raw))
			require.NotContains(t, string(raw), "private reviewer order proof")
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("proof source hook did not run")
			}
		})
	}
}

func TestMiniAppOrderReviewReadDeniesLogoutAfterEventLockWait(t *testing.T) {
	t.Parallel()
	f, scope, claim, _ := nativeOrderReviewReadFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(context.Background()) }()
	_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, readErr := f.H.store.MiniAppOrderReviewDetail(ctx, scope, session, f.Event.ID, claim.ID)
		done <- readErr
	}()
	require.Eventually(t, func() bool {
		var wait bool
		readErr := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocking.Conn().PgConn().PID()).
			Scan(&wait)
		return readErr == nil && wait
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocking.Rollback(ctx))
	select {
	case readErr := <-done:
		require.ErrorIs(t, readErr, db.ErrMiniAppDenied)
	case <-ctx.Done():
		t.Fatal("native read did not finish")
	}
}
