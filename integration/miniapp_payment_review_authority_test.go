package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMiniAppPaymentReviewHTTPInvalidationDuringEventWaitPreventsCredit(t *testing.T) {
	t.Parallel()
	f, review, _ := paymentAppFixture(t)
	f.H.peers.mu.Lock()
	f.H.peers.adminRole = true
	f.H.peers.mu.Unlock()
	admin := detailsAppSession(t, f.H, 11001, 74)
	w := paymentAppSaved(t, f, admin, review, 0)
	scope := paymentScope(t, f.H, f.AdminID, true)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	op := uuid.New()
	body := paymentAppJSON(t, map[string]any{
		"operation_id": op, "canonical_proposal_id": w.Preparation.ID,
		"arguments": workspaceCommit(w), "confirmed": true,
	})
	type outcome struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		code, raw, _, requestErr := appHTTPCall(f.H, http.MethodPost,
			paymentAppPath+"confirmations", body, admin.Token, appTestOrigin)
		done <- outcome{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := f.H.store.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))",
			blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return readErr == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	f.H.iam.Invalidate(scope.Subject)
	require.NoError(t, blocker.Rollback(ctx))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusForbidden, result.code, string(result.raw))
	case <-ctx.Done():
		t.Fatal("payment confirmation did not finish")
	}
	var reviews, calls int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, `SELECT
 (SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1),
 (SELECT count(*) FROM shuttle_payment_review_calls WHERE actor_id=$2 AND operation_id=$3)`,
		review.EvidenceID, scope.UserID, op.String()).Scan(&reviews, &calls))
	require.Zero(t, reviews, "Known invalidation must not credit money")
	require.Zero(t, calls, "Denied confirmation must not persist its receipt")
}

func TestMiniAppPaymentReviewHTTPExpiryDuringPayerWaitPreventsCredit(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	w := paymentAppSaved(t, f, session, review, 0)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_users WHERE id=$1 FOR UPDATE", f.OwnerID)
	require.NoError(t, err)
	var expires time.Time
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, `UPDATE shuttle_miniapp_sessions
 SET expires_at=clock_timestamp()+interval '3 seconds' WHERE user_id=$1 AND state='active'
 RETURNING expires_at`, f.AmbassadorID).Scan(&expires))
	op := uuid.New()
	body := paymentAppJSON(t, map[string]any{
		"operation_id": op, "canonical_proposal_id": w.Preparation.ID,
		"arguments": workspaceCommit(w), "confirmed": true,
	})
	type outcome struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		code, raw, _, requestErr := appHTTPCall(f.H, http.MethodPost,
			paymentAppPath+"confirmations", body, session.Token, appTestOrigin)
		done <- outcome{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := f.H.store.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))",
			blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return readErr == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	// The request has passed browser authorization and is blocked on the payer.
	require.Eventually(t, func() bool {
		var expired bool
		readErr := f.H.store.Pool().QueryRow(ctx, "SELECT clock_timestamp()>$1", expires).Scan(&expired)
		return readErr == nil && expired
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, blocker.Rollback(ctx))
	var result outcome
	select {
	case result = <-done:
		require.NoError(t, result.err)
	case <-ctx.Done():
		t.Fatal("payment confirmation did not finish")
	}
	var reviews, calls int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, `SELECT
 (SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1),
 (SELECT count(*) FROM shuttle_payment_review_calls WHERE actor_id=$2 AND operation_id=$3)`,
		review.EvidenceID, f.AmbassadorID, op.String()).Scan(&reviews, &calls))
	require.Zero(t, reviews, "Expired browser authority must not credit money")
	require.Zero(t, calls, "Denied confirmation must not persist its receipt")
	require.Equal(t, http.StatusForbidden, result.code, string(result.raw))
}
