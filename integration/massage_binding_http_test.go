package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const bindingWriteAPI = "/internal/agent/administration/set_massage_specialist_binding"

type bindingHTTPNoIntake struct{ t *testing.T }

func (a bindingHTTPNoIntake) Accept(context.Context, telegram.Update, json.RawMessage) (db.Task, bool, error) {
	a.t.Error("Binding HTTP must not create a Telegram intake task")
	return db.Task{}, false, errors.New("unexpected Telegram intake in binding HTTP fixture")
}

func bindingHTTPFixture(
	t *testing.T,
	singleConnection bool,
) (paymentFixture, db.ProfileScope, db.MassageBindingChange, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, _, _, _ := massageBookingFixtureWithPeers(t, 1, p)
	actor := ownerProfileScope(t, f.H, f.AdminID)
	args := db.MassageBindingChange{
		EventID:                 f.Event.ID,
		SpecialistID:            "alex",
		UserID:                  f.OwnerID,
		Active:                  true,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: 1,
	}
	p.mu.Lock()
	p.adminRole = true
	p.mu.Unlock()
	if singleConnection {
		dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
		require.NoError(t, err)
		query := dsn.Query()
		query.Set("pool_max_conns", "1")
		dsn.RawQuery = query.Encode()
		limited, err := db.Open(t.Context(), dsn.String())
		require.NoError(t, err)
		t.Cleanup(limited.Close)
		endpoint, endpointErr := api.New(limited, f.H.iam, bindingHTTPNoIntake{t}, api.Options{
			BotID:           actor.BotID,
			WebhookSecret:   "test-secret",
			OrganizationID:  actor.Organization,
			ActorID:         "actor",
			Issuer:          p.iam.URL,
			LoomAudience:    "loom",
			MiniAppOrigin:   appTestOrigin,
			MiniAppBotToken: "123:test-token",
		})
		require.NoError(t, endpointErr)
		old := f.H.store
		f.H.store = limited
		f.H.endpoint = endpoint
		old.Close()
		require.Equal(t, int32(1), limited.Pool().Config().MaxConns)
	}
	f.H.iam.Invalidate(actor.Subject)
	f.H.server = httptest.NewServer(f.H.endpoint.PublicHandler())
	session := detailsAppSession(t, f.H, 11001, 233)
	return f, actor, args, session
}

func bindingAgentPrepare(
	t *testing.T,
	f paymentFixture,
	op uuid.UUID,
	args any,
) (db.MassageBindingPreparation, map[string]any) {
	t.Helper()
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		bindingWriteAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Status        string                  `json:"status"`
		Arguments     db.MassageBindingChange `json:"arguments"`
		Description   string                  `json:"description"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Equal(t, "prepared", response.Status)
	require.Contains(t, response.Description, f.Event.Document.Name)
	require.Contains(t, response.Description, "11002")
	require.True(t, response.ExpiresAt.After(time.Now()))
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     op,
		"idempotency_key":  op,
		"arguments":        response.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": response.Preconditions.ProposalID},
		},
	}
	return db.MassageBindingPreparation{
		ID:        response.Preconditions.ProposalID,
		Arguments: response.Arguments,
		ExpiresAt: response.ExpiresAt,
	}, body
}

func TestMassageBindingHTTPNewOperationHandoffsAndOriginalStatusOnOneConnection(t *testing.T) {
	t.Parallel()
	f, _, args, session := bindingHTTPFixture(t, true)
	path := "/app/massage-admin/" + f.Event.ID.String() + "/bindings"
	nativeOp, agentOp := uuid.New(), uuid.New()
	native := massageAppCall[db.MassageBindingProposalStatus](
		t,
		f,
		session,
		http.MethodPost,
		path+"/prepare",
		map[string]any{"operation_id": nativeOp, "arguments": args},
	)
	require.Equal(t, "pending", native.State)
	rawArgs := map[string]any{}
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &rawArgs))
	rawArgs["existing_proposal_id"] = native.ID
	prepared, body := bindingAgentPrepare(t, f, agentOp, rawArgs)
	require.Equal(t, native.ID, prepared.ID)
	require.Equal(t, native.ExpiresAt, prepared.ExpiresAt)
	code, raw := agentAdminRead(t, f.H, f.Admin, "massage_specialist_pending", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var pending struct {
		Result db.MassageBindingPendingPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &pending))
	require.Len(t, pending.Result.Proposals, 1)
	page := massageAppCall[db.MassageBindingPendingPage](
		t,
		f,
		session,
		http.MethodGet,
		"/app/massage-admin/pending",
		nil,
	)
	require.Equal(t, pending.Result, page)
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, bindingWriteAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var granted struct {
		Status string                      `json:"status"`
		Result db.MassageSpecialistBinding `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &granted))
	require.Equal(t, "succeeded", granted.Status)
	require.Equal(t, int64(1), granted.Result.Version)
	receipt := massageAppCall[db.MassageBindingProposalStatus](
		t,
		f,
		session,
		http.MethodGet,
		"/app/massage-admin/status/operations/"+agentOp.String(),
		nil,
	)
	require.Equal(t, nativeOp, receipt.OperationID)
	require.Equal(t, granted.Result, *receipt.Result)
	args.Active = false
	args.ExpectedVersion = 1
	revokeOp, nativeRevokeOp := uuid.New(), uuid.New()
	revoke, _ := bindingAgentPrepare(t, f, revokeOp, args)
	adopted := massageAppCall[db.MassageBindingProposalStatus](
		t,
		f,
		session,
		http.MethodPost,
		path+"/prepare",
		map[string]any{"operation_id": nativeRevokeOp, "proposal_id": revoke.ID, "arguments": args},
	)
	require.Equal(t, revoke.ID, adopted.ID)
	require.Equal(t, revoke.ExpiresAt, adopted.ExpiresAt)
	revoked := massageAppCall[db.MassageSpecialistBinding](
		t,
		f,
		session,
		http.MethodPost,
		path,
		map[string]any{"operation_id": nativeRevokeOp, "proposal_id": revoke.ID, "arguments": args, "confirmed": true},
	)
	require.False(t, revoked.Active)
	require.Equal(t, int64(2), revoked.Version)
	for _, op := range []uuid.UUID{revokeOp, nativeRevokeOp} {
		code, raw = adminRequest(
			t,
			f.H,
			f.Admin,
			http.MethodPost,
			bindingWriteAPI+"/status",
			map[string]any{"protocol_version": 1, "operation_id": op},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.NoError(t, json.Unmarshal(raw, &granted))
		require.Equal(t, revoked, granted.Result)
	}
	replay := massageAppCall[db.MassageSpecialistBinding](
		t,
		f,
		session,
		http.MethodPost,
		path,
		map[string]any{
			"operation_id": nativeOp,
			"proposal_id":  native.ID,
			"arguments":    native.Arguments,
			"confirmed":    true,
		},
	)
	require.Equal(t, *receipt.Result, replay)
	code, raw = adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		bindingWriteAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": agentOp, "arguments": native.Arguments},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	require.NotContains(t, string(raw), `"status":"prepared"`)
	completed := massageAppCall[db.MassageBindingProposalStatus](
		t,
		f,
		session,
		http.MethodPost,
		path+"/prepare",
		map[string]any{"operation_id": nativeOp, "arguments": native.Arguments},
	)
	require.Equal(t, "completed", completed.State)
	require.Equal(t, *receipt.Result, *completed.Result)
	code, raw = agentAdminRead(t, f.H, f.Admin, "massage_specialist_proposal", map[string]any{"proposal_id": native.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"completed"`)
	f.H.peers.mu.Lock()
	require.Empty(t, f.H.peers.runs)
	f.H.peers.mu.Unlock()
}

func TestMassageBindingHTTPRejectsMissingConfirmationFieldsAndExpiredConsent(t *testing.T) {
	t.Parallel()
	f, actor, args, session := bindingHTTPFixture(t, false)
	path := "/app/massage-admin/" + f.Event.ID.String() + "/bindings"
	op := uuid.New()
	prepared, body := bindingAgentPrepare(t, f, op, args)
	bad := map[string]any{}
	encoded, err := json.Marshal(args)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &bad))
	for _, field := range []string{"active", "expected_version"} {
		value := bad[field]
		delete(bad, field)
		code, raw, _ := appCall(
			t,
			f.H,
			http.MethodPost,
			path+"/prepare",
			paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "arguments": bad}),
			session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
		bad[field] = value
	}
	for _, confirmed := range []bool{false, true} {
		input := map[string]any{"operation_id": op, "arguments": args, "confirmed": confirmed}
		code, raw, _ := appCall(t, f.H, http.MethodPost, path, paymentAppJSON(t, input), session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path,
		paymentAppJSON(
			t,
			map[string]any{"operation_id": op, "arguments": args, "proposal_id": prepared.ID, "confirmed": true},
		),
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	body["idempotency_key"] = uuid.New()
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, bindingWriteAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["idempotency_key"] = op
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET "+
			"expires_at=clock_timestamp()-interval '1 second' WHERE actor_id=$1", actor.UserID)
	require.NoError(t, err)
	code, raw = adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		bindingWriteAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, bindingWriteAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	status := massageAppCall[db.MassageBindingProposalStatus](
		t,
		f,
		session,
		http.MethodGet,
		"/app/massage-admin/status/proposals/"+prepared.ID.String(),
		nil,
	)
	require.Equal(t, "expired", status.State)
	page := massageAppCall[db.MassageBindingPendingPage](
		t,
		f,
		session,
		http.MethodGet,
		"/app/massage-admin/pending?event_id="+f.Event.ID.String(),
		nil,
	)
	require.Empty(t, page.Proposals)
	for _, query := range []string{
		"/app/massage-admin/status/operations/nope",
		"/app/massage-admin/pending?after=nope",
		"/app/massage-admin/status/proposals/" + uuid.Nil.String(),
	} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, query, "", session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	for _, a := range []map[string]any{{}, {"operation_id": op, "proposal_id": prepared.ID}} {
		code, raw = agentAdminRead(t, f.H, f.Admin, "massage_specialist_proposal", a)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
}

func TestMassageBindingHTTPRequiresCurrentAdminForWritesPendingAndReceipts(t *testing.T) {
	t.Parallel()
	f, actor, args, session := bindingHTTPFixture(t, false)
	op := uuid.New()
	prepared, body := bindingAgentPrepare(t, f, op, args)
	for path, envelope := range map[string]any{
		bindingWriteAPI + "/prepare": map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args},
		bindingWriteAPI:              body,
		bindingWriteAPI + "/status":  map[string]any{"protocol_version": 1, "operation_id": op},
	} {
		code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, path, envelope)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	for method, a := range map[string]any{
		"massage_specialist_pending":  map[string]any{},
		"massage_specialist_proposal": map[string]any{"proposal_id": prepared.ID},
	} {
		code, raw := agentAdminRead(t, f.H, f.Owner, method, a)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	p := f.H.peers
	p.mu.Lock()
	p.adminRole = false
	p.mu.Unlock()
	f.H.iam.Invalidate(actor.Subject)
	root := "/app/massage-admin/" + f.Event.ID.String() + "/bindings"
	for _, path := range []string{root + "/prepare", root} {
		code, raw, _ := appCall(
			t,
			f.H,
			http.MethodPost,
			path,
			paymentAppJSON(
				t,
				map[string]any{
					"operation_id": op,
					"arguments":    args,
					"proposal_id":  prepared.ID,
					"confirmed":    path == root,
				},
			),
			session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	for _, path := range []string{
		"/app/massage-admin/pending",
		"/app/massage-admin/status/operations/" + op.String(),
		"/app/massage-admin/status/proposals/" + prepared.ID.String(),
	} {
		code, raw, _ := appCall(t, f.H, http.MethodGet, path, "", session.Token, appTestOrigin)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
}

func TestMassageBindingHTTPRechecksAdminAfterWaitingForEvent(t *testing.T) {
	t.Parallel()
	f, actor, args, session := bindingHTTPFixture(t, false)
	op := uuid.New()
	prep, _ := bindingAgentPrepare(t, f, op, args)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer blocker.Rollback(ctx)
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", args.EventID)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		f.H.server.URL+"/app/massage-admin/"+args.EventID.String()+"/bindings",
		strings.NewReader(
			paymentAppJSON(
				t,
				map[string]any{"operation_id": op, "arguments": args, "proposal_id": prep.ID, "confirmed": true},
			),
		),
	)
	require.NoError(t, err)
	req.Header.Set("Authorization", "ZinusyaSession "+session.Token)
	req.Header.Set("Origin", appTestOrigin)
	req.Header.Set("Content-Type", "application/json")
	type response struct {
		code int
		body string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		res, e := f.H.server.Client().Do(req)
		if e != nil {
			done <- response{err: e}
			return
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		done <- response{res.StatusCode, string(raw), e}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND "+
				"pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	f.H.peers.mu.Lock()
	f.H.peers.adminRole = false
	f.H.peers.mu.Unlock()
	f.H.iam.Invalidate(actor.Subject)
	require.NoError(t, blocker.Commit(ctx))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusForbidden, result.code, result.body)
	case <-ctx.Done():
		t.Fatal("binding HTTP remained blocked")
	}
	var count int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_specialist_bindings").Scan(&count),
	)
	require.Zero(t, count)
}
