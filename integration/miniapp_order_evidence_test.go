package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMiniAppOrderEvidenceSharesOriginalReceiptWithAgentAndRestart(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	f.H.stop(t)
	args := orderEvidenceArgs(t, f, order, 61601, "native-owner-proof")
	session := nativeOrderSession(t, f, scope, 11002)
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	op := uuid.New()
	first, err := f.H.store.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidencePending, first.State)
	agent, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	require.Equal(t, first, agent)
	_, _, confirmed := reviewOrderEvidence(t, f, first, db.OrderEvidenceConfirmed)
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	receipt, err := reopened.MiniAppOrderEvidenceSubmissionReceipt(t.Context(), deployment, session, f.Event.ID, op)
	require.NoError(t, err)
	require.Equal(t, first, receipt.Result)
	current, err := reopened.MiniAppOwnOrderEvidence(t.Context(), deployment, session, f.Event.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, confirmed, current)
	replay, err := reopened.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
	require.NoError(t, reopened.LogoutMiniAppSession(t.Context(), scope.BotID, session.Bearer, session.Generation))
	_, err = reopened.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = reopened.MiniAppOrderEvidenceSubmissionReceipt(t.Context(), deployment, session, f.Event.ID, op)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = reopened.MiniAppOwnOrderEvidence(t.Context(), deployment, session, f.Event.ID, first.ID)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}

func TestMiniAppOrderEvidenceLogoutDuringEventWaitPreventsAllEffects(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	f.H.stop(t)
	args := orderEvidenceArgs(t, f, order, 61602, "native-session-wait")
	session := nativeOrderSession(t, f, scope, 11002)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(context.Background()) }()
	_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, writeErr := f.H.store.SubmitMiniAppOrderEvidence(
			ctx,
			db.EventScope{BotID: scope.BotID, Organization: scope.Organization},
			session,
			uuid.New(),
			args,
		)
		done <- writeErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return readErr == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocking.Rollback(ctx))
	select {
	case writeErr := <-done:
		require.ErrorIs(t, writeErr, db.ErrMiniAppDenied)
	case <-ctx.Done():
		t.Fatal("native evidence write did not finish")
	}
	var evidence, receipts, notices int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT (SELECT count(*) FROM shuttle_order_evidence),(SELECT count(*) FR" +
"OM shuttle_order_evidence_operations),(SELECT count(*) FROM shuttle_orde" +
"r_evidence_notices)").
			Scan(&evidence, &receipts, &notices),
	)
	require.Zero(t, evidence)
	require.Zero(t, receipts)
	require.Zero(t, notices)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
}

func TestMiniAppOrderEvidenceFilePagesAreOwnerScopedAndDoNotSubmitClaims(t *testing.T) {
	t.Parallel()
	f, scope, _ := orderEvidenceFixture(t)
	f.H.stop(t)
	expected := []string{}
	for n := range 23 {
		expected = append(
			expected,
			paymentFile(
				t,
				f.H,
				f.OwnerID,
				int64(61610+n),
				fmt.Sprintf("native-file-%d", n),
				time.Now().UTC().Truncate(time.Second),
			).String(),
		)
	}
	foreign := paymentFile(t, f.H, f.AmbassadorID, 61650, "foreign-native-file", time.Now().UTC().Truncate(time.Second))
	session := nativeOrderSession(t, f, scope, 11002)
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	after := uuid.Nil
	actual := []string{}
	for range 12 {
		page, err := f.H.store.MiniAppOrderEvidenceFileList(t.Context(), deployment, session, f.Event.ID, after, 2)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Files), 2)
		for _, file := range page.Files {
			require.NotEqual(t, foreign, file.ID)
			actual = append(actual, file.ID.String())
		}
		if page.Next == nil {
			break
		}
		require.Equal(t, page.Files[len(page.Files)-1].ID, *page.Next)
		after = *page.Next
	}
	sort.Strings(expected)
	require.Equal(t, expected, actual)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	require.NoError(t, f.H.store.LogoutMiniAppSession(t.Context(), scope.BotID, session.Bearer, session.Generation))
	_, err := f.H.store.MiniAppOrderEvidenceFileList(t.Context(), deployment, session, f.Event.ID, uuid.Nil, 2)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}

func TestMiniAppOrderEvidenceHTTPSubmissionRecoveryAndExplicitTarget(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, owner, args := orderApprovalFixture(t, p, false)
	stopCtx, stopCancel := context.WithTimeout(t.Context(), time.Second)
	require.NoError(t, f.H.service.Stop(stopCtx))
	stopCancel()
	f.H.service = nil
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), owner, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	order, err := f.H.store.SubmitOwnOrder(t.Context(), owner, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	proof := orderEvidenceArgs(t, f, order.Order, 61660, "native-http-source")
	session := detailsAppSession(t, f.H, 11002, 113)
	path := ordersAppPath(f.Event.ID)
	code, raw, headers := appCall(t, f.H, http.MethodGet, path+"/proof-files", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.Contains(t, string(raw), proof.FileID.String())
	require.NotContains(t, string(raw), "telegram_id")
	operation := uuid.New()
	body := map[string]any{
		"operation_id":           operation,
		"order_id":               proof.OrderID,
		"expected_order_version": proof.ExpectedOrderVersion,
		"file_id":                proof.FileID,
		"amount_minor":           proof.AmountMinor,
	}
	encoded := paymentAppJSON(t, body)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/payment-evidence", encoded, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var first db.OrderPaymentEvidence
	require.NoError(t, json.Unmarshal(raw, &first))
	require.Equal(t, db.OrderEvidenceExhausted, first.State)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/payment-evidence-operations/"+operation.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt db.OrderEvidenceOperation
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, first, receipt.Result)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/payment-evidence", encoded, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var retry db.OrderPaymentEvidence
	require.NoError(t, json.Unmarshal(raw, &retry))
	require.Equal(t, first, retry)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/payment-evidence/"+first.ID.String(), "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var current db.OrderPaymentEvidence
	require.NoError(t, json.Unmarshal(raw, &current))
	require.Equal(t, first, current)
	body["amount_minor"] = proof.AmountMinor + 1
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/payment-evidence",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	delete(body, "amount_minor")
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/payment-evidence",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/payment-evidence",
		encoded,
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	p.mu.Lock()
	require.Empty(t, p.createAttempts)
	p.mu.Unlock()
}

func TestMiniAppOrderEvidenceHTTPDiscoversAgentClaimInFreshSession(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, owner, args := orderApprovalFixture(t, p, false)
	stopCtx, stopCancel := context.WithTimeout(t.Context(), time.Second)
	require.NoError(t, f.H.service.Stop(stopCtx))
	stopCancel()
	f.H.service = nil
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), owner, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	order, err := f.H.store.SubmitOwnOrder(t.Context(), owner, op, args, prep.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	proof := orderEvidenceArgs(t, f, order.Order, 61670, "agent-native-fresh")
	operation := uuid.New()
	var agent orderWireResult[db.OrderPaymentEvidence]
	require.NoError(t, json.Unmarshal(orderEvidenceWrite(t, f, f.Owner, "submit", operation, proof, uuid.Nil), &agent))
	require.Equal(t, "succeeded", agent.Status)
	require.NotNil(t, agent.Result)
	session := detailsAppSession(t, f.H, 11002, 114)
	path := ordersAppPath(f.Event.ID) + "/payment-evidence"
	code, raw, _ := appCall(t, f.H, http.MethodGet, path+"?order_id="+order.Order.ID.String(), "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.OrderEvidencePage
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Evidence, 1)
	require.Equal(t, *agent.Result, page.Evidence[0])
	foreign := detailsAppSession(t, f.H, 11003, 115)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"?order_id="+order.Order.ID.String(), "", foreign.Token, "")
	require.NotEqual(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), agent.Result.ID.String())
}
