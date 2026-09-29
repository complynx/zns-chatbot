package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestHistoryLazyRetirementPreservesBatchContinuation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"clean", "older_events", "current_event", "current_summary", "explicit_delete"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			db, registration := bookingFixture(t)
			history := conversation.Service{DB: db}
			oldIDs, currentID := prepareLazyHistory(t, history, mode)
			command, source := interruptLazyHistoryBatch(t, db, registration, mode)
			reconcileLazyHistory(t, history, mode, oldIDs, currentID)
			assertLazyHistoryContinuation(t, db, registration, history, mode, command, source)
		})
	}
}

func lazyHistoryID(t *testing.T, history conversation.Service, owner, key string) int64 {
	t.Helper()
	var id int64
	require.NoError(
		t,
		history.DB.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner=$1 AND source_key=$2`, owner, key).
			Scan(&id),
	)
	return id
}

func lazyHistoryRead(t *testing.T, history conversation.Service, id int64) conversation.Page {
	t.Helper()
	page, err := history.Read(t.Context(), "bob", conversation.Query{After: id - 1, Before: id + 1, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	return page
}

func lazyHistorySource(generation int64) []readsource.Authority {
	return []readsource.Authority{
		{
			Causal: &readsource.CausalSource{
				Actor:       "alice",
				Generation:  &generation,
				Authorities: []readsource.Authority{},
			},
		},
	}
}

func prepareLazyHistory(t *testing.T, history conversation.Service, mode string) ([]int64, int64) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, history.AppendOriginal(ctx, "bob", "prior-delete", "user", "prior source"))
	require.NoError(t, history.DeleteContent(ctx, "bob", lazyHistoryID(t, history, "bob", "prior-delete")))
	require.NoError(t, history.AppendOriginal(ctx, "alice", "source", "user", "first source"))
	require.NoError(t, history.AppendDerived(ctx, "bob", "dependent", "first derived source", 1, lazyHistorySource(0)))
	dependent := lazyHistoryID(t, history, "bob", "dependent")
	var oldIDs []int64
	if mode == "older_events" {
		require.NoError(
			t,
			history.AppendDerived(ctx, "bob", "older-short", "old independent summary", 1, []readsource.Authority{}),
		)
		require.NoError(
			t,
			history.AppendDerived(
				ctx,
				"bob",
				"older-body",
				strings.Repeat("independent body ", conversation.MaxTextBytes),
				1,
				[]readsource.Authority{},
			),
		)
		oldIDs = []int64{
			lazyHistoryID(t, history, "bob", "older-short"),
			lazyHistoryID(t, history, "bob", "older-body"),
		}
		window, err := history.Window(ctx, "bob", 1)
		require.NoError(t, err)
		require.NoError(
			t,
			history.CommitSummary(ctx, "bob", window.Summary.Version, oldIDs[:1], "old independent summary"),
		)
	}
	require.NoError(t, history.DeleteContent(ctx, "alice", lazyHistoryID(t, history, "alice", "source")))
	retired := lazyHistoryRead(t, history, dependent)
	require.Equal(t, int64(2), retired.Generation)
	require.True(t, retired.Events[0].Omitted)
	var summary string
	require.NoError(
		t,
		history.DB.QueryRow(ctx, `SELECT text FROM core.conversation_summaries WHERE owner='bob'`).Scan(&summary),
	)
	require.Empty(t, summary)
	if mode != "current_event" && mode != "current_summary" {
		return oldIDs, 0
	}
	require.NoError(t, history.AppendOriginal(ctx, "alice", "current-source", "user", "new source"))
	require.NoError(
		t,
		history.AppendDerived(ctx, "bob", "current-derived", "current derived body", 2, lazyHistorySource(1)),
	)
	id := lazyHistoryID(t, history, "bob", "current-derived")
	if mode == "current_summary" {
		window, err := history.Window(ctx, "bob", 1)
		require.NoError(t, err)
		require.NoError(
			t,
			history.CommitSummary(ctx, "bob", window.Summary.Version, []int64{id}, "current derived summary"),
		)
		require.NoError(t, history.AppendOriginal(ctx, "bob", "recent-original", "user", "recent independent original"))
	}
	return oldIDs, id
}

func interruptLazyHistoryBatch(
	t *testing.T,
	db *pgxpool.Pool,
	registration passbooking.Service,
	mode string,
) (passbooking.RuntimeBatch, readsource.Derivation) {
	t.Helper()
	ctx := t.Context()
	_, err := db.Exec(
		ctx,
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
 VALUES('dance','alice',1,'assigned','leader','solo','bob',now(),now(),100),('dance','bob',1,'assigned','follower','solo','bob',now(),now(),100)`,
	)
	require.NoError(t, err)
	price := 101
	command := passbooking.RuntimeBatch{
		Key:        "lazy-" + mode,
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101, 202},
		Options:    passbooking.AdminAssignment{TotalPrice: &price},
	}
	generation := int64(2)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	service := derivedmutation.Service{DB: db, Registration: registration}
	locker, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = locker.Rollback(context.Background()) }()
	var lockerPID int
	require.NoError(t, locker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&lockerPID))
	_, err = locker.Exec(ctx, `SELECT owner FROM core.pass_bookings WHERE event_id='dance' AND owner='bob' FOR SHARE`)
	require.NoError(t, err)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, runErr := service.RunPassBatch(runCtx, "bob", command, source); done <- runErr }()
	require.Eventually(t, func() bool {
		var receipts, waiters int
		queryErr := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.pass_admin_assignments),(SELECT count(*) FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, lockerPID).
			Scan(&receipts, &waiters)
		return queryErr == nil && receipts == 1 && waiters == 1
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	select {
	case runErr := <-done:
		require.Error(t, runErr)
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted batch did not return")
	}
	require.NoError(t, locker.Rollback(ctx))
	return command, source
}

func reconcileLazyHistory(t *testing.T, history conversation.Service, mode string, oldIDs []int64, currentID int64) {
	t.Helper()
	ctx := t.Context()
	for _, id := range oldIDs {
		for range 2 {
			page := lazyHistoryRead(t, history, id)
			require.Equal(t, int64(2), page.Generation)
			require.True(t, page.Events[0].Omitted)
			require.Equal(t, "authority_revoked", page.Events[0].OmissionReason)
			require.Empty(t, page.Events[0].Text)
			require.False(t, page.Events[0].HasFullText)
		}
	}
	if mode == "explicit_delete" {
		require.NoError(t, history.AppendOriginal(ctx, "bob", "new-delete", "user", "explicitly retired"))
		require.NoError(t, history.DeleteContent(ctx, "bob", lazyHistoryID(t, history, "bob", "new-delete")))
	}
	if currentID != 0 {
		require.NoError(t, history.DeleteContent(ctx, "alice", lazyHistoryID(t, history, "alice", "current-source")))
		if mode == "current_summary" {
			window, err := history.Window(ctx, "bob", 1)
			require.NoError(t, err)
			require.Equal(t, int64(3), window.Generation)
			require.Empty(t, window.Summary.Text)
			require.False(t, window.Recent[0].Omitted)
		}
		page := lazyHistoryRead(t, history, currentID)
		require.Equal(t, int64(3), page.Generation)
		require.True(t, page.Events[0].Omitted)
	}
}

func assertLazyHistoryContinuation(
	t *testing.T,
	db *pgxpool.Pool,
	registration passbooking.Service,
	history conversation.Service,
	mode string,
	command passbooking.RuntimeBatch,
	source readsource.Derivation,
) {
	t.Helper()
	ctx := t.Context()
	expectedGeneration := int64(2)
	expectedStatus := passbooking.AdminBatchSucceeded
	expectedReceipts := 2
	if mode != "clean" && mode != "older_events" {
		expectedGeneration = 3
		expectedStatus = passbooking.AdminBatchRejected
		expectedReceipts = 1
	}
	generation, err := history.Generation(ctx, "bob")
	require.NoError(t, err)
	require.Equal(t, expectedGeneration, generation)
	require.Equal(t, int64(2), *source.Generation, "admitted source is immutable")
	service := derivedmutation.Service{DB: db, Registration: registration}
	for range 2 {
		items, resumeErr := service.RunManualPassBatch(ctx, "bob", command)
		require.NoError(t, resumeErr)
		require.Len(t, items, 2)
		require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
		require.Equal(t, expectedStatus, items[1].Outcome.Status)
		if expectedStatus == passbooking.AdminBatchRejected {
			require.Equal(t, "history_stale", items[1].Outcome.Code)
		}
		var firstVersion, secondVersion, receipts int
		require.NoError(
			t,
			db.QueryRow(ctx, `SELECT (SELECT version FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'),(SELECT version FROM core.pass_bookings WHERE event_id='dance' AND owner='bob'),(SELECT count(*) FROM core.pass_admin_assignments)`).
				Scan(&firstVersion, &secondVersion, &receipts),
		)
		require.Equal(t, 2, firstVersion)
		require.Equal(t, expectedReceipts, secondVersion)
		require.Equal(t, expectedReceipts, receipts)
	}
}
