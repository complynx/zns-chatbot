package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

func staffHTTPFixture(t *testing.T) (paymentFixture, db.ProfileScope, string, db.MassageScheduleChange, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, _, _, schedule := massageBookingFixtureWithPeers(t, 2, p)
	id, token := registrationUser(t, f.H, 11444, false)
	staff := ownerProfileScope(t, f.H, id)
	bindingChange(
		t,
		f,
		ownerProfileScope(t, f.H, f.AdminID),
		db.MassageBindingChange{
			EventID:                 f.Event.ID,
			SpecialistID:            "alex",
			UserID:                  id,
			Active:                  true,
			ExpectedEventVersion:    f.Event.Version,
			ExpectedScheduleVersion: 1,
		},
	)
	f.H.server = httptest.NewServer(f.H.endpoint.PublicHandler())
	session := detailsAppSession(t, f.H, 11444, 231)
	return f, staff, token, schedule, session
}

func staffAgentRead[T any](t *testing.T, f paymentFixture, token, method string, args any) T {
	t.Helper()
	code, raw := adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		"/internal/agent/massage_staff/"+method,
		map[string]any{"protocol_version": 1, "operation_id": "staff-read", "arguments": args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var out struct {
		Result T `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out.Result
}

func TestMassageStaffHTTPAgentAndNativeShareNoPassTimetableAndOwnContacts(t *testing.T) {
	t.Parallel()
	f, staff, token, schedule, session := staffHTTPFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	other := ownerProfileScope(t, f.H, f.AmbassadorID)
	first, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	args := massageBookingArgs(f, schedule, 0)
	args.Selection.SpecialistID = "beth"
	second, _ := bookMassage(t, f, other, args)
	var tasksBefore, tasksAfter int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasksBefore))
	found := staffAgentRead[events.ContextList](t, f, token, "events", map[string]any{})
	require.Len(t, found.Events, 1)
	require.Equal(t, f.Event.ID, found.Events[0].ID)
	nativeEvents := massageAppCall[events.ContextList](t, f, session, http.MethodGet, "/app/massage-staff/events", nil)
	require.Equal(t, found, nativeEvents)
	view := staffAgentRead[db.MassageStaffScheduleView](t, f, token, "schedule", map[string]any{"event_id": f.Event.ID})
	require.Equal(t, []string{"alex"}, view.OwnSpecialistIDs)
	path := "/app/massage-staff/" + f.Event.ID.String()
	nativeView := massageAppCall[db.MassageStaffScheduleView](t, f, session, http.MethodGet, path+"/schedule", nil)
	require.Equal(t, view.Document, nativeView.Document)
	windows := staffAgentRead[db.MassageStaffWindowsPage](
		t,
		f,
		token,
		"windows",
		db.MassageStaffWindowsQuery{EventID: f.Event.ID},
	)
	nativeWindows := massageAppCall[db.MassageStaffWindowsPage](
		t,
		f,
		session,
		http.MethodPost,
		path+"/windows",
		db.MassageStaffWindowsQuery{EventID: f.Event.ID},
	)
	require.Equal(t, windows, nativeWindows)
	require.Len(t, windows.Windows, 1, "Current and frozen identical window instants must deduplicate")
	require.Equal(t, schedule.Document.Windows[0].Name, windows.Windows[0].Name)
	query := staffQuery(f, schedule)
	page := staffAgentRead[db.MassageStaffPage](t, f, token, "timetable", query)
	defaultPage := staffAgentRead[db.MassageStaffPage](t, f, token, "timetable", map[string]any{
		"event_id": query.EventID, "window_id": query.WindowID, "from": query.From, "until": query.Until,
	})
	require.Equal(
		t,
		page.Bookings,
		defaultPage.Bookings,
		"Omitting own_only must select the event-wide timetable as advertised",
	)
	nativePage := massageAppCall[db.MassageStaffPage](t, f, session, http.MethodPost, path+"/timetable", query)
	require.Len(t, page.Bookings, 2)
	require.Equal(t, page.Bookings, nativePage.Bookings)
	own := staffAgentRead[db.MassageStaffBooking](
		t,
		f,
		token,
		"client",
		map[string]any{"event_id": f.Event.ID, "booking_id": first.ID},
	)
	nativeOwn := massageAppCall[db.MassageStaffBooking](
		t,
		f,
		session,
		http.MethodGet,
		path+"/clients/"+first.ID.String(),
		nil,
	)
	require.Equal(t, own, nativeOwn)
	require.Equal(t, "tg://user?id=11002", own.Contact)
	code, _ := adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		"/internal/agent/massage_staff/client",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "staff-read",
			"arguments":        map[string]any{"event_id": f.Event.ID, "booking_id": second.ID},
		},
	)
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = appCall(t, f.H, http.MethodGet, path+"/clients/"+second.ID.String(), "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		"/internal/agent/massage_staff/schedule",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "staff-read",
			"arguments":        map[string]any{"event_id": f.Event.ID},
		},
	)
	require.Equal(t, http.StatusNotFound, code, "Assigned participant pass alone is not staff identity")
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasksAfter))
	require.Equal(t, tasksBefore, tasksAfter, "Native reads must not start agent work")
	revoke := db.MassageBindingChange{
		EventID:                 f.Event.ID,
		SpecialistID:            "alex",
		UserID:                  staff.UserID,
		Active:                  false,
		ExpectedVersion:         1,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: 1,
	}
	bindingChange(t, f, ownerProfileScope(t, f.H, f.AdminID), revoke)
	code, _, _ = appCall(t, f.H, http.MethodGet, path+"/schedule", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		"/internal/agent/massage_staff/timetable",
		map[string]any{"protocol_version": 1, "operation_id": "staff-read", "arguments": query},
	)
	require.Equal(t, http.StatusNotFound, code)
}

func TestMassageBindingHTTPAdminReadsResolveOnlyKnownScopedTarget(t *testing.T) {
	t.Parallel()
	f, staff, token, _, session := staffHTTPFixture(t)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	f.H.peers.mu.Lock()
	f.H.peers.adminRole = true
	f.H.peers.mu.Unlock()
	f.H.iam.Invalidate(admin.Subject)
	adminSession := detailsAppSession(t, f.H, 11001, 232)
	root := "/app/massage-admin/" + f.Event.ID.String()
	found := massageAppCall[events.ContextList](t, f, adminSession, http.MethodGet, "/app/massage-admin/events", nil)
	require.Len(t, found.Events, 1)
	view := massageAppCall[db.MassageScheduleView](t, f, adminSession, http.MethodGet, root+"/schedule", nil)
	require.Equal(t, int64(1), view.Version)
	page := massageAppCall[db.MassageBindingPage](t, f, adminSession, http.MethodGet, root+"/bindings?limit=1", nil)
	require.Len(t, page.Bindings, 1)
	require.Equal(t, staff.UserID, page.Bindings[0].UserID)
	require.True(t, page.ModuleEnabled)
	binding := massageAppCall[db.MassageSpecialistBinding](
		t,
		f,
		adminSession,
		http.MethodGet,
		root+"/bindings/alex",
		nil,
	)
	require.Equal(t, page.Bindings[0], binding)
	target := massageAppCall[db.MassageBindingTarget](
		t,
		f,
		adminSession,
		http.MethodPost,
		"/app/massage-admin/target",
		map[string]any{"telegram_user_id": 11444},
	)
	require.Equal(t, staff.UserID, target.UserID)
	require.True(t, target.Linked)
	require.False(t, target.Revoked)
	code, raw := agentAdminRead(t, f.H, f.Admin, "massage_specialist_target", map[string]any{"user_id": staff.UserID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), staff.Subject)
	require.NotContains(t, string(raw), "zitadel")
	var result struct {
		Result db.MassageBindingTarget `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, target, result.Result)
	for method, arguments := range map[string]any{
		"massage_specialist_events":   map[string]any{},
		"massage_specialist_bindings": map[string]any{"event_id": f.Event.ID},
		"massage_specialist_binding":  map[string]any{"event_id": f.Event.ID, "specialist_id": "alex"},
		"massage_specialist_target":   map[string]any{"user_id": staff.UserID},
	} {
		code, _ = agentAdminRead(t, f.H, token, method, arguments)
		require.Equal(t, http.StatusForbidden, code, method)
	}

	code, _, _ = appCall(t, f.H, http.MethodGet, root+"/bindings", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusForbidden, code)
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		"/app/massage-admin/target",
		`{"telegram_user_id":11444}`,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusForbidden, code)
	for _, input := range []map[string]any{
		{},
		{"user_id": staff.UserID, "telegram_user_id": 11444},
		{"telegram_user_id": -1},
	} {
		code, _, _ = appCall(
			t,
			f.H,
			http.MethodPost,
			"/app/massage-admin/target",
			paymentAppJSON(t, input),
			adminSession.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code)
	}
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		"/app/massage-admin/target",
		`{"telegram_user_id":999999}`,
		adminSession.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusNotFound, code)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	page = massageAppCall[db.MassageBindingPage](t, f, adminSession, http.MethodGet, root+"/bindings", nil)
	require.False(t, page.ModuleEnabled)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
	require.NoError(t, err)
	target = massageAppCall[db.MassageBindingTarget](
		t,
		f,
		adminSession,
		http.MethodPost,
		"/app/massage-admin/target",
		map[string]any{"user_id": staff.UserID},
	)
	require.True(t, target.Revoked)
}

func TestMassageStaffHTTPRejectsCrossEventOriginAndExpiredSession(t *testing.T) {
	t.Parallel()
	f, _, token, schedule, session := staffHTTPFixture(t)
	path := "/app/massage-staff/" + f.Event.ID.String()
	query := staffQuery(f, schedule)
	code, _, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/timetable",
		paymentAppJSON(t, query),
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code)
	query.EventID = uuid.New()
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/timetable",
		paymentAppJSON(t, query),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code)
	query = staffQuery(f, schedule)
	query.Limit = 21
	code, raw := adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		"/internal/agent/massage_staff/timetable",
		map[string]any{"protocol_version": 1, "operation_id": "staff-read", "arguments": query},
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 "+
			"second' WHERE user_id=$1", session.UserID)
	require.NoError(t, err)
	code, _, _ = appCall(t, f.H, http.MethodGet, path+"/schedule", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code)
	code, _, _ = appCall(t, f.H, http.MethodGet, "/app/massage-staff/events", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code)
	code, _, _ = appCall(t, f.H, http.MethodGet, "/app/massage-admin/events", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestMiniAppMassageBindingSameOperationSharesEffectsAndChecksCurrentSession(t *testing.T) {
	t.Parallel()
	f, admin, args, _ := bindingFixture(t)
	session := nativeMassageSession(t, f, admin, 11001)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMiniAppMassageBinding(t.Context(), admin, session, operation, args)
	require.NoError(t, err)
	first, err := f.H.store.ChangeMassageBinding(
		t.Context(),
		admin,
		operation,
		prepared.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	native, err := f.H.store.MiniAppMassageBindingOperation(t.Context(), admin, session, operation)
	require.NoError(t, err)
	require.Equal(t, first, native)
	args.Active = false
	args.ExpectedVersion = first.Version
	nextOp := uuid.New()
	next, err := f.H.store.PrepareMassageBinding(t.Context(), admin, nextOp, args)
	require.NoError(t, err)
	revoked, err := f.H.store.ChangeMiniAppMassageBinding(
		t.Context(),
		admin,
		session,
		nextOp,
		next.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.False(t, revoked.Active)
	original := prepared.Arguments
	replay, err := f.H.store.ChangeMiniAppMassageBinding(
		t.Context(),
		admin,
		session,
		operation,
		prepared.ID,
		original,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	require.NoError(t, f.H.store.LogoutMiniAppSession(t.Context(), admin.BotID, session.Bearer, session.Generation))
	_, err = f.H.store.ChangeMiniAppMassageBinding(
		t.Context(),
		admin,
		session,
		operation,
		prepared.ID,
		original,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.MiniAppMassageBindingOperation(t.Context(), admin, session, operation)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}

func TestMiniAppMassageBindingRejectsLogoutAfterWaitingForEvent(t *testing.T) {
	t.Parallel()
	f, admin, args, _ := bindingFixture(t)
	session := nativeMassageSession(t, f, admin, 11001)
	op := uuid.New()
	prepared, err := f.H.store.PrepareMiniAppMassageBinding(t.Context(), admin, session, op, args)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.H.store.ChangeMiniAppMassageBinding(
			ctx,
			admin,
			session,
			op,
			prepared.ID,
			args,
			func(context.Context) error { return nil },
		)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND "+
				"pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, admin.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocker.Commit(ctx))
	select {
	case err = <-done:
		require.ErrorIs(t, err, db.ErrMiniAppDenied)
	case <-ctx.Done():
		t.Fatal("Binding write remained blocked")
	}
	var count int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_specialist_bindings").Scan(&count),
	)
	require.Zero(t, count)
}
