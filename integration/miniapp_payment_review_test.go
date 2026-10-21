package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func nativePaymentSession(
	t *testing.T,
	f paymentFixture,
	scope db.PaymentScope,
	telegramID int64,
) db.MiniAppPaymentSession {
	t.Helper()
	proof := sha256.Sum256([]byte(uuid.NewString()))
	nonce := sha256.Sum256([]byte(uuid.NewString()))
	generation := sha256.Sum256([]byte("native-payment-test-key"))
	bearer := sha256.Sum256([]byte(uuid.NewString()))
	attempt, err := f.H.store.ClaimMiniAppSession(t.Context(), db.MiniAppClaim{
		BotID: scope.BotID, TelegramUserID: telegramID, Organization: scope.Organization,
		Profile: json.RawMessage(`{}`), ProofHash: proof, NonceHash: nonce, Generation: generation,
		AuthDate: time.Now().UTC().Truncate(time.Second), MaxAge: 5 * time.Minute,
		PendingTTL: time.Minute, SessionTTL: 30 * time.Minute,
	})
	require.NoError(t, err)
	require.Equal(t, scope.UserID, attempt.User.ID)
	_, err = f.H.store.ActivateMiniAppSession(t.Context(), db.MiniAppActivation{
		ID: attempt.ID, BotID: scope.BotID, NonceHash: nonce, Generation: generation, BearerHash: bearer,
		Subject: scope.Subject, Organization: scope.Organization,
	})
	require.NoError(t, err)
	return db.MiniAppPaymentSession{
		Bearer:             bearer,
		Generation:         generation,
		AuthorityUnchanged: func() bool { return true },
	}
}

func nativeReviewRequest(w db.PaymentReviewWorkspace) db.MiniAppPaymentReviewCommit {
	return db.MiniAppPaymentReviewCommit{Arguments: workspaceCommit(w), CanonicalProposalID: w.Preparation.ID}
}

func TestMiniAppPaymentReviewHandoffsAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	f, scope, review := reviewWorkspaceFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	ctx := t.Context()
	w, err := f.H.store.PrepareMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		"native-save",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	args := workspaceCommit(w)
	agent, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "agent-commit", args)
	require.NoError(t, err)
	original, err := f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "agent-commit", args, agent.ID)
	require.NoError(t, err)
	// An already completed/expired decision is recovered through a new native alias.
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_payment_preparations SET expires_at=clock_timestamp()-interval '1 hour' WHERE proposal_id=$1`, w.Preparation.ID)
	require.NoError(t, err)
	request := nativeReviewRequest(w)
	native, err := f.H.store.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "native-confirm", request)
	require.NoError(t, err)
	require.Equal(t, original, native)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	retry, err := reopened.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "native-confirm", request)
	require.NoError(t, err)
	require.Equal(t, original, retry)
	status, err := reopened.MiniAppPaymentReviewWorkspaceOperation(ctx, scope, session, "agent-commit")
	require.NoError(t, err)
	require.Equal(t, original, *status.Review)
	status, err = reopened.PaymentReviewWorkspaceOperation(ctx, scope, "native-confirm")
	require.NoError(t, err)
	require.Equal(t, original, *status.Review)
	changed := request
	changed.CanonicalProposalID = uuid.New()
	_, err = reopened.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "native-confirm", changed)
	require.ErrorIs(t, err, db.ErrConflict)
	changed = request
	changed.Arguments.Review.AmountMinor++
	_, err = reopened.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "native-confirm", changed)
	require.ErrorIs(t, err, db.ErrConflict)
	// A write cannot claim the other transport's operation or create another journal entry.
	_, err = reopened.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"native-save",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	var reviews, notices, nativeActions int
	require.NoError(t, reopened.Pool().QueryRow(ctx, `SELECT
  (SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1),
  (SELECT count(*) FROM shuttle_payment_notifications WHERE evidence_id=$1 AND kind='confirmed'),
  (SELECT count(*) FROM shuttle_payment_review_calls WHERE actor_id=$2 AND source='miniapp' AND completed_at IS NOT NULL)`, review.EvidenceID, scope.UserID).Scan(&reviews, &notices, &nativeActions))
	require.Equal(t, 1, reviews)
	require.Equal(t, 1, notices)
	require.Equal(t, 2, nativeActions)
}

func TestMiniAppPaymentReviewCancelAndAgentToNative(t *testing.T) {
	t.Parallel()
	f, scope, review := reviewWorkspaceFixture(t)
	ctx := t.Context()
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"agent-save",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	cancel := db.PaymentReviewWorkspaceChange{WorkspaceID: w.ID, Revision: w.Revision}
	cancelled, err := f.H.store.CancelMiniAppPaymentReviewWorkspace(ctx, scope, session, "native-cancel", cancel)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	_, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		"cancelled-confirm",
		nativeReviewRequest(w),
	)
	require.ErrorIs(t, err, db.ErrConflict)
	review.AmountMinor = 3500
	replacement, err := f.H.store.PrepareMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		"native-edit",
		db.PaymentReviewDraftChange{Review: review, ExpectedRevision: 1},
	)
	require.NoError(t, err)
	old, err := f.H.store.CancelMiniAppPaymentReviewWorkspace(ctx, scope, session, "native-cancel", cancel)
	require.NoError(t, err)
	require.Equal(t, cancelled, old)
	result, err := f.H.store.CommitMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		"native-confirm",
		nativeReviewRequest(replacement),
	)
	require.NoError(t, err)
	require.EqualValues(t, 3500, result.Review.Balance.ConfirmedMinor)
}

func TestMiniAppPaymentReviewSessionRequiredOnReplay(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "expiry", "key-generation", "foreign-owner"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, scope, review := reviewWorkspaceFixture(t)
			ctx := t.Context()
			session := nativePaymentSession(t, f, scope, 11003)
			draft := db.PaymentReviewDraftChange{Review: review}
			w, err := f.H.store.PrepareMiniAppPaymentReviewWorkspace(ctx, scope, session, "save", draft)
			require.NoError(t, err)
			request := nativeReviewRequest(w)
			_, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "confirm", request)
			require.NoError(t, err)
			switch kind {
			case "logout":
				require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
			case "expiry":
				_, err = f.H.store.Pool().
					Exec(ctx, `UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE bearer_hash=$1`, session.Bearer[:])
				require.NoError(t, err)
			case "key-generation":
				session.Generation = sha256.Sum256([]byte("different-key"))
			case "foreign-owner":
				session = nativePaymentSession(t, f, paymentScope(t, f.H, f.OwnerID, false), 11002)
			}
			_, err = f.H.store.PrepareMiniAppPaymentReviewWorkspace(ctx, scope, session, "save", draft)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "confirm", request)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = f.H.store.CancelMiniAppPaymentReviewWorkspace(
				ctx,
				scope,
				session,
				"cancel",
				db.PaymentReviewWorkspaceChange{WorkspaceID: w.ID, Revision: w.Revision},
			)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = f.H.store.MiniAppPaymentReviewWorkspaceOperation(ctx, scope, session, "confirm")
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
		})
	}
}

func TestMiniAppPaymentReviewRollbackIsAtomic(t *testing.T) {
	t.Parallel()
	f, scope, review := reviewWorkspaceFixture(t)
	ctx := t.Context()
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"agent-save",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, `CREATE FUNCTION fail_native_receipt() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'fixture receipt write failure'; END$$;
 CREATE TRIGGER fail_native_receipt BEFORE UPDATE OF result ON shuttle_payment_review_calls FOR EACH ROW EXECUTE FUNCTION fail_native_receipt()`)
	require.NoError(t, err)
	_, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "confirm", nativeReviewRequest(w))
	require.Error(t, err)
	balance, err := f.H.store.PaymentBalance(ctx, paymentScope(t, f.H, f.OwnerID, false), review.EventID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
	require.Zero(t, balance.Version)
	var effects int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, `SELECT
 (SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1)+
 (SELECT count(*) FROM shuttle_payment_notifications WHERE evidence_id=$1 AND kind='confirmed')+
 (SELECT count(*) FROM shuttle_payment_review_calls WHERE actor_id=$2 AND operation_id='confirm')`, review.EvidenceID, scope.UserID).Scan(&effects))
	require.Zero(t, effects)
	_, err = f.H.store.Pool().
		Exec(ctx, `DROP TRIGGER fail_native_receipt ON shuttle_payment_review_calls; DROP FUNCTION fail_native_receipt()`)
	require.NoError(t, err)
	_, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(ctx, scope, session, "confirm", nativeReviewRequest(w))
	require.NoError(t, err)
}

func TestMiniAppPaymentReviewConcurrentEventsAndLogout(t *testing.T) {
	t.Parallel()
	f, scope, first := reviewWorkspaceFixture(t)
	secondEvent := registrationEvent(t, f.H, f.Admin)
	entry := registerForEvent(t, f.H, f.Owner, secondEvent.ID, "second-join", "leader")
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		assignmentPath(secondEvent.ID, entry.ID),
		assignmentBody("second-assign", secondEvent, entry),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &entry))
	require.NotNil(t, entry.Pass)
	adminScope := paymentScope(t, f.H, f.AdminID, true)
	_, err := f.H.store.ChangeAuthority(
		t.Context(),
		adminScope.EventScope,
		adminScope.Subject,
		"second-ambassador",
		db.AuthorityChange{EventID: secondEvent.ID, UserID: scope.UserID, Role: db.AuthorityAmbassador},
		uuid.Nil,
	)
	require.NoError(t, err)
	secondFixture := f
	secondFixture.Event = secondEvent
	secondFixture.Pass = *entry.Pass
	file := paymentFile(t, f.H, f.OwnerID, 19002, "second-native-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, secondFixture, "second-proof", file, 4000, payments.Initial)
	second := first
	second.EventID = secondEvent.ID
	second.EvidenceID = evidence.ID
	session := nativePaymentSession(t, f, scope, 11003)
	firstW, err := f.H.store.PreparePaymentReviewWorkspace(
		t.Context(),
		scope,
		"first-save",
		db.PaymentReviewDraftChange{Review: first},
	)
	require.NoError(t, err)
	secondW, err := f.H.store.PreparePaymentReviewWorkspace(
		t.Context(),
		scope,
		"second-save",
		db.PaymentReviewDraftChange{Review: second},
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(
		ctx,
		`SELECT id FROM shuttle_miniapp_sessions WHERE bearer_hash=$1 FOR UPDATE`,
		session.Bearer[:],
	)
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(2)
	errors := make([]error, 2)
	for i, w := range []db.PaymentReviewWorkspace{firstW, secondW} {
		go func() {
			defer wg.Done()
			_, errors[i] = f.H.store.CommitMiniAppPaymentReviewWorkspace(
				ctx,
				scope,
				session,
				w.ID.String(),
				nativeReviewRequest(w),
			)
		}()
	}
	// Both event transactions reach the same session lock while retaining actor SHARE.
	require.Eventually(t, func() bool {
		var count int
		queryErr := f.H.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE '%FROM shuttle_miniapp_sessions WHERE id=%'`).
			Scan(&count)
		return queryErr == nil && count == 2
	}, 5*time.Second, 20*time.Millisecond)
	logoutResult := make(chan error, 1)
	go func() {
		logoutResult <- f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation)
	}()
	// Logout waits for the two in-flight financial transactions' actor locks.
	require.Eventually(t, func() bool {
		var count int
		queryErr := f.H.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE '%FROM shuttle_users WHERE id=%FOR UPDATE%'`).
			Scan(&count)
		return queryErr == nil && count == 1
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, blocker.Commit(ctx))
	wg.Wait()
	require.NoError(t, errors[0])
	require.NoError(t, errors[1])
	require.NoError(t, <-logoutResult)
	_, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		"late-retry",
		nativeReviewRequest(firstW),
	)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}
