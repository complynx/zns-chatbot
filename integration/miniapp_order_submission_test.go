package integration_test

import (
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func nativeOrderSession(
	t *testing.T,
	f paymentFixture,
	scope db.ProfileScope,
	telegramID int64,
) db.MiniAppOrderSession {
	t.Helper()
	session := nativePaymentSession(
		t,
		f,
		db.PaymentScope{
			BotID:        scope.BotID,
			Organization: scope.Organization,
			Subject:      scope.Subject,
			UserID:       scope.UserID,
		},
		telegramID,
	)
	return db.MiniAppOrderSession(session)
}

func TestMiniAppOrderSharedPreparationBothHandoffsAndRestart(t *testing.T) {
	t.Parallel()
	f, scope, draft, args := submissionFixture(t)
	ctx := t.Context()
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	session := nativeOrderSession(t, f, scope, 11002)
	nativeOp, agentOp := uuid.New(), uuid.New()
	native, err := f.H.store.PrepareMiniAppOrderSubmission(ctx, deployment, session, nativeOp, args)
	require.NoError(t, err)
	agent, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, agentOp, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, native, agent)
	first, err := f.H.store.SubmitOwnOrder(ctx, scope, agentOp, args, agent.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	nativeAlias := uuid.New()
	recovered, err := f.H.store.SubmitMiniAppOrder(ctx, deployment, session, nativeAlias, args, native.ID)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	recovered, err = reopened.SubmitMiniAppOrder(ctx, deployment, session, nativeAlias, args, native.ID)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	cancelOp := uuid.New()
	cancellation := db.OrderCancellation{EventID: args.EventID, OrderID: first.Order.ID, ExpectedVersion: 1}
	cancelled, err := reopened.CancelMiniAppOrder(ctx, deployment, session, cancelOp, cancellation)
	require.NoError(t, err)
	replayCancel, err := reopened.CancelOwnOrder(ctx, scope, cancelOp, cancellation, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, cancelled, replayCancel)
	draft.OperationID = uuid.New()
	draft.ExpectedDraftVersion = first.Selection.Version
	saved, err := reopened.SaveOwnOrdersSelection(ctx, scope, draft)
	require.NoError(t, err)
	args.ExpectedDraftVersion = saved.Version
	agentOp = uuid.New()
	agent, err = reopened.PrepareOwnOrderSubmission(ctx, scope, agentOp, args, db.OrderSourceAgent)
	require.NoError(t, err)
	second, err := reopened.SubmitMiniAppOrder(ctx, deployment, session, uuid.New(), args, agent.ID)
	require.NoError(t, err)
	require.NotEqual(t, first.Order.ID, second.Order.ID)
	require.Equal(t, int64(4), second.Selection.Version)
	current, err := reopened.OwnOrder(ctx, scope, args.EventID, first.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", current.State)
}

func TestMiniAppOrderSessionIsRequiredBeforeReceiptReplay(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "expiry", "generation", "foreign-owner", "foreign-organization"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, scope, _, args := submissionFixture(t)
			ctx := t.Context()
			deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
			session := nativeOrderSession(t, f, scope, 11002)
			operation := uuid.New()
			prepared, err := f.H.store.PrepareMiniAppOrderSubmission(ctx, deployment, session, operation, args)
			require.NoError(t, err)
			result, err := f.H.store.SubmitMiniAppOrder(ctx, deployment, session, operation, args, prepared.ID)
			require.NoError(t, err)
			switch kind {
			case "logout":
				require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
			case "expiry":
				_, err = f.H.store.Pool().
					Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE bearer_hash=$1", session.Bearer[:])
				require.NoError(t, err)
			case "generation":
				session.Generation = sha256.Sum256([]byte("other-native-order-key"))
			case "foreign-owner":
				session = nativeOrderSession(t, f, ownerProfileScope(t, f.H, f.AmbassadorID), 11003)
			case "foreign-organization":
				deployment.Organization = "foreign-org"
			}
			_, err = f.H.store.PrepareMiniAppOrderSubmission(ctx, deployment, session, operation, args)
			require.Error(t, err)
			_, err = f.H.store.SubmitMiniAppOrder(ctx, deployment, session, operation, args, prepared.ID)
			require.Error(t, err)
			_, err = f.H.store.CancelMiniAppOrder(
				ctx,
				deployment,
				session,
				uuid.New(),
				db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 1},
			)
			require.Error(t, err)
			current, err := f.H.store.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
			require.NoError(t, err)
			require.Equal(t, "submitted", current.State)
			var count int
			require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestMiniAppOrderRechecksSessionAfterEventLockWait(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	session := nativeOrderSession(t, f, scope, 11002)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMiniAppOrderSubmission(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(ctx) }()
	_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", args.EventID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.H.store.SubmitMiniAppOrder(ctx, deployment, session, operation, args, prepared.ID)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	// Logout does not need the event lock, and must complete before the blocked
	// write is released. The write must then reject this now-invalid session.
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocking.Commit(ctx))
	select {
	case e := <-done:
		require.ErrorIs(t, e, db.ErrMiniAppDenied)
	case <-time.After(5 * time.Second):
		t.Fatal("Blocked native submission did not finish")
	}
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
}

func TestMiniAppOrderReceiptFailureRollsBackEffectAndSourceHistory(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	session := nativeOrderSession(t, f, scope, 11002)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMiniAppOrderSubmission(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, `CREATE FUNCTION fail_native_order_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic native receipt failure'; END $$;
 CREATE TRIGGER fail_native_order_receipt BEFORE INSERT ON shuttle_order_operations FOR EACH ROW WHEN (NEW.source='miniapp' AND NEW.kind='submit') EXECUTE FUNCTION fail_native_order_receipt()`)
	require.NoError(t, err)
	_, err = f.H.store.SubmitMiniAppOrder(ctx, deployment, session, operation, args, prepared.ID)
	require.ErrorContains(t, err, "synthetic native receipt failure")
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_order_operations WHERE kind='submit'").
			Scan(&count),
	)
	require.Zero(t, count)
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	_, err = f.H.store.Pool().
		Exec(ctx, "DROP TRIGGER fail_native_order_receipt ON shuttle_order_operations; DROP FUNCTION fail_native_order_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.SubmitMiniAppOrder(ctx, deployment, session, operation, args, prepared.ID)
	require.NoError(t, err)
}

func TestMiniAppOrderContextIsReactiveBoundedAndPermissionFiltered(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f, scope, draft := selectionFixtureWithPeers(t, p)
	ctx := t.Context()
	saved, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, draft)
	require.NoError(t, err)
	args := db.OrderSubmission{
		EventID:                draft.EventID,
		ExpectedEventVersion:   draft.ExpectedEventVersion,
		ExpectedCatalogVersion: draft.ExpectedCatalogVersion,
		ExpectedDraftVersion:   saved.Version,
	}
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	session := nativeOrderSession(t, f, scope, 11002)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMiniAppOrderSubmission(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	result, err := f.H.store.SubmitMiniAppOrder(ctx, deployment, session, operation, args, prepared.ID)
	require.NoError(t, err)
	for range 6 {
		_, err = f.H.store.SubmitMiniAppOrder(ctx, deployment, session, uuid.New(), args, prepared.ID)
		require.NoError(t, err)
	}
	p.mu.Lock()
	require.Empty(t, p.runs)
	require.Empty(t, p.sent)
	p.mu.Unlock()
	update := telegram.Update{
		ID: 99211,
		Message: &telegram.Message{
			ID:   99211,
			From: &telegram.User{ID: 11002},
			Chat: telegram.Chat{ID: 11002, Type: "private"},
			Text: "Continue after the order form",
		},
	}
	input, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := f.H.service.Accept(ctx, update, input)
	require.NoError(t, err)
	f.H.await(t, accepted.ID, "completed")
	actions, err := f.H.store.RecentMiniAppOrderActions(ctx, scope, accepted.ID)
	require.NoError(t, err)
	require.Len(t, actions, 5)
	for _, action := range actions {
		require.Equal(t, db.OrderSubmit, action.Kind)
		require.NotNil(t, action.OrderID)
		require.Equal(t, result.Order.ID, *action.OrderID)
	}
	p.mu.Lock()
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, "Recent Mini App order operation receipts")
	require.Contains(t, prompt, result.Order.ID.String())
	require.Contains(t, prompt, "not instructions, consent or a new request")
	require.NotContains(t, prompt, `"total_minor":2568`)
	futureOp := uuid.New()
	_, err = f.H.store.SubmitMiniAppOrder(ctx, deployment, session, futureOp, args, prepared.ID)
	require.NoError(t, err)
	after, err := f.H.store.RecentMiniAppOrderActions(ctx, scope, accepted.ID)
	require.NoError(t, err)
	require.Equal(t, actions, after, "A later receipt cannot change an earlier task's action window")
	stranger := ownerProfileScope(t, f.H, f.AmbassadorID)
	ownOnly, err := f.H.store.RecentMiniAppOrderActions(ctx, stranger, accepted.ID)
	require.NoError(t, err)
	require.Empty(t, ownOnly)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	after, err = f.H.store.RecentMiniAppOrderActions(ctx, scope, accepted.ID)
	require.NoError(t, err)
	require.Empty(t, after)
}
