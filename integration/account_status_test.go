package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestAccountStatusPreservesWaitlistsPaidHistoryAndPendingMoney(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	waitEvent := registrationEvent(t, f.H, f.Admin)
	wait := registerForEvent(t, f.H, f.Owner, waitEvent.ID, uuid.NewString(), registrations.Follower)
	f.H.stop(t)
	ctx := context.Background()
	scope := ownerProfileScope(t, f.H, f.OwnerID)
	read := func() db.AccountEvents {
		result, err := f.H.store.OwnAccountEvents(ctx, scope, uuid.Nil, 0)
		require.NoError(t, err)
		require.Len(t, result.Events, 2)
		return result
	}
	find := func(page db.AccountEvents, id uuid.UUID) db.AccountEvent {
		for _, item := range page.Events {
			if item.EventID == id {
				return item
			}
		}
		t.Fatal("Owned event missing")
		return db.AccountEvent{}
	}
	page := read()
	queued := find(page, waitEvent.ID)
	require.Equal(t, wait.ID, queued.Registration.ID)
	require.Equal(t, registrations.Waitlisted, queued.Registration.State)
	require.Nil(t, queued.Pass)
	assigned := find(page, f.Event.ID)
	require.NotNil(t, assigned.Pass)
	require.Zero(t, assigned.Pass.Balance.ConfirmedMinor)
	require.EqualValues(t, 12345, assigned.Pass.Balance.RemainingMinor)
	file := paymentFile(t, f.H, f.OwnerID, 79801, "account-pending", time.Now().UTC().Truncate(time.Second))
	_, err := f.H.store.SubmitPaymentEvidence(
		ctx,
		paymentScope(t, f.H, f.OwnerID, false),
		"account-pending",
		payments.Submission{
			EventID: f.Event.ID, PassID: f.Pass.ID, FileID: file, AmountMinor: 3087,
			Currency: "BYN", CurrencyScale: 2, Purpose: payments.Initial},
	)
	require.NoError(t, err)
	assigned = find(read(), f.Event.ID)
	require.EqualValues(t, 1, assigned.Pass.PendingEvidence)
	require.Zero(t, assigned.Pass.Balance.ConfirmedMinor)
	confirmExpectationPayment(t, f, 3087, payments.Initial, 0)
	assigned = find(read(), f.Event.ID)
	require.EqualValues(t, 3087, assigned.Pass.Balance.ConfirmedMinor)
	require.EqualValues(t, 9258, assigned.Pass.Balance.RemainingMinor)
	require.Equal(t, payments.TopUp, assigned.Pass.Purpose)
	confirmExpectationPayment(t, f, 9258, payments.TopUp, 1)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET starts_at=now()-interval '2 days',ends_at=now(" +
")-interval '1 day' WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	assigned = find(read(), f.Event.ID)
	require.True(t, assigned.Pass.Balance.FullyPaid)
	require.Zero(t, assigned.Pass.Balance.RemainingMinor)
	require.False(t, assigned.Pass.Overdue)
	require.True(t, assigned.EndsAt.Before(page.ObservedAt))
	_, offset := assigned.StartsAt.Zone()
	require.Equal(t, 3*60*60, offset)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_events SET modules='{}' WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	assigned = find(read(), f.Event.ID)
	require.False(t, assigned.RegistrationEnabled)
	require.Equal(t, registrations.Assigned, assigned.Registration.State)
	require.Nil(t, assigned.Pass, "Disabled module must not expose invented zero balances")
}

func TestAccountStatusIsOwnerOnlyBoundedAndWorksWithOneConnection(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	for range 2 {
		event := registrationEvent(t, f.H, f.Admin)
		registerForEvent(t, f.H, f.Owner, event.ID, uuid.NewString(), registrations.Leader)
	}
	f.H.stop(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	parsed, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	single, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	defer single.Close()
	scope := ownerProfileScope(t, f.H, f.OwnerID)
	var ids []uuid.UUID
	after := uuid.Nil
	for {
		page, readErr := single.OwnAccountEvents(ctx, scope, after, 1)
		require.NoError(t, readErr)
		require.Len(t, page.Events, 1)
		ids = append(ids, page.Events[0].EventID)
		if !page.HasMore {
			require.Nil(t, page.Next)
			break
		}
		require.NotNil(t, page.Next)
		after = *page.Next
	}
	require.Len(t, ids, 3)
	require.NotEqual(t, ids[0], ids[1])
	require.NotEqual(t, ids[1], ids[2])
	empty, err := single.OwnAccountEvents(ctx, ownerProfileScope(t, f.H, f.AdminID), uuid.Nil, 0)
	require.NoError(t, err)
	require.Empty(t, empty.Events, "Global administrators do not inherit other owners' records")
	for _, bad := range []db.ProfileScope{
		{BotID: 124, Organization: scope.Organization, Subject: scope.Subject, UserID: scope.UserID},
		{BotID: scope.BotID, Organization: "foreign", Subject: scope.Subject, UserID: scope.UserID},
		{BotID: scope.BotID, Organization: scope.Organization, Subject: "foreign", UserID: scope.UserID},
	} {
		_, err = single.OwnAccountEvents(ctx, bad, uuid.Nil, 0)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	_, err = single.OwnAccountEvents(ctx, scope, uuid.Nil, 21)
	require.ErrorIs(t, err, registrations.ErrInvalid)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = single.OwnAccountEvents(ctx, scope, uuid.Nil, 0)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestMiniAppAccountAndAgentListShareOwnerStatusWithoutWrites(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f := setupPayment(t, p)
	code, raw, _ := appCall(t, f.H, http.MethodPost, appBootstrapPath, appBody(appProof(11002), 7), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/account", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.AccountEvents
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Events, 1)
	require.Equal(t, f.Pass.ID, page.Events[0].Pass.Balance.PassID)
	require.NotContains(t, string(raw), "zitadel")
	require.NotContains(t, string(raw), "user_id")
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, "/internal/agent/registrations/list", map[string]any{
		"protocol_version": 1, "operation_id": "account-read", "arguments": map[string]any{}})
	require.Equal(t, http.StatusOK, code, string(raw))
	var agent struct {
		Result db.AccountEvents `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &agent))
	require.Equal(t, page.Events, agent.Result.Events)
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, "/internal/agent/registrations/list", map[string]any{
		"protocol_version": 1, "operation_id": "admin-own-account-read", "arguments": map[string]any{}})
	require.Equal(t, http.StatusOK, code, string(raw))
	var adminPage struct {
		Result db.AccountEvents `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &adminPage))
	require.Empty(t, adminPage.Result.Events)
	for _, query := range []string{"?limit=21", "?after=bad", "?limit=1&limit=2", "?user_id=" + f.AdminID.String(), "?after=%zz"} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/account"+query, "", session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	f.H.stop(t)
	f.H.start(t)
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/account", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, "/app/account", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
	var tasks int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(context.Background(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks),
	)
	require.Zero(t, tasks)
}

func TestMiniAppAccountSuppressesDataWhenSessionExpiresDuringRead(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f := setupPayment(t, p)
	code, raw, _ := appCall(t, f.H, http.MethodPost, appBootstrapPath, appBody(appProof(11002), 9), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	ctx := context.Background()
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(ctx) }()
	_, err = blocking.Exec(ctx, "LOCK TABLE shuttle_passes IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	type reply struct {
		code int
		raw  []byte
		err  error
	}
	finished := make(chan reply, 1)
	go func() {
		status, result, _, requestErr := appHTTPCall(
			f.H,
			http.MethodGet,
			"/app/account",
			"",
			session.Token,
			appTestOrigin,
		)
		finished <- reply{status, result, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := f.H.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks
WHERE relation='shuttle_passes'::regclass AND NOT granted)`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interva" +
"l '1 second' WHERE user_id=$1", f.OwnerID)
	require.NoError(t, err)
	require.NoError(t, blocking.Commit(ctx))
	select {
	case result := <-finished:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
		require.NotContains(t, string(result.raw), f.Event.Document.Name)
	case <-time.After(5 * time.Second):
		t.Fatal("Account read did not finish after releasing its DB wait")
	}
}
