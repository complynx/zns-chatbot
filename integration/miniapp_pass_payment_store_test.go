package integration_test

import (
	"context"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func passPaymentStoreFixture(
	t *testing.T,
) (paymentFixture, db.PaymentScope, db.MiniAppPaymentEvidenceSession, db.MiniAppPaymentEvidenceSubmission) {
	t.Helper()
	f := setupPayment(t, newPeers(t))
	scope := paymentScope(t, f.H, f.OwnerID, false)
	native := nativePaymentSession(t, f, scope, 11002)
	session := db.MiniAppPaymentEvidenceSession{
		UserID:             scope.UserID,
		Bearer:             native.Bearer,
		Generation:         native.Generation,
		AuthorityUnchanged: func() bool { return true },
	}
	file := paymentFile(t, f.H, f.OwnerID, 51001, "native-pass-initial", time.Now().UTC().Truncate(time.Second))
	args := db.MiniAppPaymentEvidenceSubmission{
		PassID:      f.Pass.ID,
		FileID:      file,
		AmountMinor: f.Pass.Deposit.RequiredMinor,
		Purpose:     payments.Initial,
	}
	return f, scope, session, args
}

func TestMiniAppPassPaymentStoreDepositTopUpAndOneLease(t *testing.T) {
	t.Parallel()
	f, scope, session, args := passPaymentStoreFixture(t)
	dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	store, err := db.Open(t.Context(), dsn.String())
	require.NoError(t, err)
	defer store.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_files SET sent_at=clock_timestamp()-interval '2 days' WHERE id=$1`, args.FileID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_passes SET assigned_at=clock_timestamp()-interval '3 days" +
"',payment_due_at=clock_timestamp()-interval '1 day' WHERE id=$1", f.Pass.ID)
	require.NoError(t, err)
	initial, err := store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "native-initial", f.Event.ID, args)
	require.NoError(t, err)
	require.Equal(t, payments.Pending, initial.State)
	target, err := store.MiniAppOwnPaymentTarget(ctx, scope, session, f.Event.ID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, target.Balance.ConfirmedMinor)
	require.EqualValues(t, 1, target.PendingEvidence)
	targets, err := store.MiniAppPaymentTargets(ctx, scope, session, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, targets.Targets, 1)
	files, err := store.MiniAppPaymentEvidenceFiles(ctx, scope, session, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, files.Files, 1)
	source, err := store.MiniAppPaymentEvidenceSource(ctx, scope, session, f.Event.ID, args.FileID)
	require.NoError(t, err)
	require.Equal(t, args.FileID, source.ID)
	claims, err := store.MiniAppOwnPaymentEvidenceList(ctx, scope, session, f.Event.ID, f.Pass.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, claims.Evidence, 1)
	receipt, err := store.MiniAppPaymentSubmissionReceipt(ctx, scope, session, f.Event.ID, "native-initial")
	require.NoError(t, err)
	require.Equal(t, initial, receipt.Result)
	// A timely file can be reviewed after the event's sales window and initial due date.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET starts_at=clock_timestamp()-interval '3 days'," +
"ends_at=clock_timestamp()-interval '1 day' WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	authority := paymentScope(t, f.H, f.AmbassadorID, false)
	review := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  initial.ID,
		Decision:    payments.Confirmed,
		AmountMinor: args.AmountMinor,
	}
	proposal, err := f.H.store.PreparePaymentReview(ctx, authority, "native-initial-review", review)
	require.NoError(t, err)
	confirmed, err := f.H.store.ReviewPaymentEvidence(ctx, authority, "native-initial-review", review, proposal.ID)
	require.NoError(t, err)
	require.True(t, confirmed.Balance.DepositSatisfied)
	require.False(t, confirmed.Balance.FullyPaid)
	// The first stage and frozen terms survive a changed balance and process lease.
	replay, err := store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "native-initial", f.Event.ID, args)
	require.NoError(t, err)
	require.Equal(t, initial, replay)
	current, err := store.MiniAppOwnPaymentEvidence(ctx, scope, session, f.Event.ID, initial.ID)
	require.NoError(t, err)
	require.Equal(t, payments.Confirmed, current.State)
	topUp := args
	topUp.FileID = paymentFile(t, f.H, f.OwnerID, 51002, "native-pass-topup", time.Now().UTC().Truncate(time.Second))
	topUp.Purpose = payments.TopUp
	topUp.AmountMinor = confirmed.Balance.RemainingMinor
	final, err := store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "native-final", f.Event.ID, topUp)
	require.NoError(t, err)
	review = payments.ReviewChange{
		EventID:         f.Event.ID,
		EvidenceID:      final.ID,
		Decision:        payments.Confirmed,
		AmountMinor:     topUp.AmountMinor,
		ExpectedVersion: confirmed.Balance.Version,
	}
	proposal, err = f.H.store.PreparePaymentReview(ctx, authority, "native-final-review", review)
	require.NoError(t, err)
	paid, err := f.H.store.ReviewPaymentEvidence(ctx, authority, "native-final-review", review, proposal.ID)
	require.NoError(t, err)
	require.True(t, paid.Balance.FullyPaid)
	targets, err = store.MiniAppPaymentTargets(ctx, scope, session, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, targets.Targets, 1)
	require.True(t, targets.Targets[0].Balance.FullyPaid)
	recent, err := store.MiniAppRecentPaymentEvidenceActions(ctx, scope, session)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	missing, err := store.MiniAppPaymentSubmissionReceipt(ctx, scope, session, f.Event.ID, "unknown")
	require.Error(t, err)
	require.Equal(t, uuid.Nil, missing.Result.ID)
}

func TestMiniAppPassPaymentStoreAgentReceiptCollisionAndOwnership(t *testing.T) {
	t.Parallel()
	f, scope, session, args := passPaymentStoreFixture(t)
	ctx := t.Context()
	full := payments.Submission{
		EventID:       f.Event.ID,
		PassID:        args.PassID,
		FileID:        args.FileID,
		AmountMinor:   args.AmountMinor,
		Purpose:       args.Purpose,
		Currency:      f.Pass.Currency,
		CurrencyScale: f.Pass.CurrencyScale,
	}
	original, err := f.H.store.SubmitPaymentEvidence(ctx, scope, "agent-original", full)
	require.NoError(t, err)
	native, err := f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "agent-original", f.Event.ID, args)
	require.NoError(t, err)
	require.Equal(t, original, native)
	_, err = f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "source-reused", f.Event.ID, args)
	require.ErrorIs(t, err, db.ErrConflict)
	changed := args
	changed.AmountMinor++
	_, err = f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "agent-original", f.Event.ID, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	changed = args
	changed.Purpose = payments.TopUp
	_, err = f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "agent-original", f.Event.ID, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	recent, err := f.H.store.MiniAppRecentPaymentEvidenceActions(ctx, scope, session)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	require.Equal(t, "agent-original", recent[0].OperationID)
	admin := paymentScope(t, f.H, f.AdminID, true)
	adminNative := nativePaymentSession(t, f, admin, 11001)
	adminSession := db.MiniAppPaymentEvidenceSession{
		UserID:             admin.UserID,
		Bearer:             adminNative.Bearer,
		Generation:         adminNative.Generation,
		AuthorityUnchanged: func() bool { return true },
	}
	_, err = f.H.store.MiniAppOwnPaymentTarget(ctx, admin, adminSession, f.Event.ID, f.Pass.ID)
	require.Error(t, err)
	_, err = f.H.store.MiniAppPaymentEvidenceSource(ctx, admin, adminSession, f.Event.ID, args.FileID)
	require.Error(t, err)
	_, err = f.H.store.MiniAppOwnPaymentEvidence(ctx, admin, adminSession, f.Event.ID, original.ID)
	require.Error(t, err)
	_, err = f.H.store.MiniAppPaymentSubmissionReceipt(ctx, admin, adminSession, f.Event.ID, "agent-original")
	require.Error(t, err)
	_, err = f.H.store.SubmitMiniAppPaymentEvidence(ctx, admin, adminSession, "foreign-pass", f.Event.ID, args)
	require.Error(t, err)
}

func TestMiniAppPassPaymentStoreSessionChangesDuringEventWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "revoked", "local-fence"} {
		for _, replay := range []bool{false, true} {
			name := kind + "/new"
			if replay {
				name = kind + "/replay"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f, scope, session, args := passPaymentStoreFixture(t)
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				const operation = "waiting-native"
				if replay {
					_, err := f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, operation, f.Event.ID, args)
					require.NoError(t, err)
				}
				var valid atomic.Bool
				valid.Store(true)
				session.AuthorityUnchanged = valid.Load
				blocker, err := f.H.store.Pool().Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = blocker.Rollback(context.Background()) }()
				_, err = blocker.Exec(ctx, `SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE`, f.Event.ID)
				require.NoError(t, err)
				outcome := make(chan error, 1)
				go func() {
					_, callErr := f.H.store.SubmitMiniAppPaymentEvidence(
						ctx,
						scope,
						session,
						operation,
						f.Event.ID,
						args,
					)
					outcome <- callErr
				}()
				require.Eventually(t, func() bool {
					var waiting int
					queryErr := f.H.store.Pool().
						QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_" +
"pids(pid))", blocker.Conn().PgConn().PID()).
						Scan(&waiting)
					return queryErr == nil && waiting >= 1
				}, 5*time.Second, 20*time.Millisecond)
				switch kind {
				case "logout":
					require.NoError(
						t,
						f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation),
					)
				case "revoked":
					_, err = f.H.store.Pool().
						Exec(ctx, `UPDATE shuttle_users SET revoked=true WHERE id=$1`, scope.UserID)
					require.NoError(t, err)
				case "local-fence":
					valid.Store(false)
				}
				require.NoError(t, blocker.Commit(ctx))
				require.Error(t, <-outcome)
				var count int
				require.NoError(
					t,
					f.H.store.Pool().
						QueryRow(ctx, `SELECT count(*) FROM shuttle_payment_evidence WHERE user_id=$1`, scope.UserID).
						Scan(&count),
				)
				expected := 0
				if replay {
					expected = 1
				}
				require.Equal(t, expected, count)
				_, err = f.H.store.MiniAppPaymentSubmissionReceipt(ctx, scope, session, f.Event.ID, operation)
				require.Error(t, err)
			})
		}
	}
}

func TestMiniAppPassPaymentStoreHistoryUsesFirstNativeAcceptanceAndTaskBoundary(t *testing.T) {
	t.Parallel()
	f, scope, session, args := passPaymentStoreFixture(t)
	ctx := t.Context()
	var before uuid.UUID
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, `SELECT task_id FROM shuttle_files WHERE id=$1`, args.FileID).Scan(&before),
	)
	original, err := f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "native-history", f.Event.ID, args)
	require.NoError(t, err)
	first, err := f.H.store.MiniAppRecentPaymentEvidenceActions(ctx, scope, session)
	require.NoError(t, err)
	require.Len(t, first, 1)
	profile := db.ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		Subject:      scope.Subject,
		UserID:       scope.UserID,
	}
	old, err := f.H.store.RecentNativePaymentEvidenceActions(ctx, profile, before)
	require.NoError(t, err)
	require.Empty(t, old)
	// A new accepted input can refresh an already-running agent conversation.
	nextFile := paymentFile(t, f.H, f.OwnerID, 51003, "history-next-input", time.Now().UTC().Truncate(time.Second))
	var after uuid.UUID
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, `SELECT task_id FROM shuttle_files WHERE id=$1`, nextFile).Scan(&after),
	)
	actions, err := f.H.store.RecentNativePaymentEvidenceActions(ctx, profile, after)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, original.ID, actions[0].EvidenceID)
	again, err := f.H.store.SubmitMiniAppPaymentEvidence(ctx, scope, session, "native-history", f.Event.ID, args)
	require.NoError(t, err)
	require.Equal(t, original, again)
	recent, err := f.H.store.MiniAppRecentPaymentEvidenceActions(ctx, scope, session)
	require.NoError(t, err)
	require.Equal(t, first, recent)
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_events SET modules=array_remove(modules,'registrations') WHERE id=$1`, f.Event.ID)
	require.NoError(t, err)
	filtered, err := f.H.store.MiniAppRecentPaymentEvidenceActions(ctx, scope, session)
	require.NoError(t, err)
	require.Empty(t, filtered)
	actions, err = f.H.store.RecentNativePaymentEvidenceActions(ctx, profile, after)
	require.NoError(t, err)
	require.Empty(t, actions)
}

func TestMiniAppPassPaymentStoreReceiptSurvivesUnavailableAssignedTarget(t *testing.T) {
	t.Parallel()
	f, scope, session, args := passPaymentStoreFixture(t)
	original, err := f.H.store.SubmitMiniAppPaymentEvidence(
		t.Context(),
		scope,
		session,
		"historical-target",
		f.Event.ID,
		args,
	)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_registrations SET state='waitlisted',version=version+1 WH" +
"ERE id=$1", f.Pass.RegistrationID)
	require.NoError(t, err)
	_, err = f.H.store.MiniAppOwnPaymentTarget(t.Context(), scope, session, f.Event.ID, f.Pass.ID)
	require.Error(t, err)
	receipt, err := f.H.store.MiniAppPaymentSubmissionReceipt(
		t.Context(),
		scope,
		session,
		f.Event.ID,
		"historical-target",
	)
	require.NoError(t, err)
	require.Equal(t, original, receipt.Result)
	require.Equal(t, f.Event.Document.Timezone, receipt.Timezone)
	replay, err := f.H.store.SubmitMiniAppPaymentEvidence(
		t.Context(),
		scope,
		session,
		"historical-target",
		f.Event.ID,
		args,
	)
	require.NoError(t, err)
	require.Equal(t, original, replay)
	_, err = f.H.store.SubmitMiniAppPaymentEvidence(
		t.Context(),
		scope,
		session,
		"new-after-target-unavailable",
		f.Event.ID,
		args,
	)
	require.Error(t, err)
}
