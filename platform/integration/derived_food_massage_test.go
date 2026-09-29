package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func foodMassageSource(t *testing.T, db *pgxpool.Pool, actor string) readsource.Derivation {
	t.Helper()
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('',$1,'review')`,
		actor,
	)
	require.NoError(t, err)
	generation := int64(0)
	return readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
}

func TestDerivedFoodSourceReceiptAndHistory(t *testing.T) {
	t.Parallel()
	f, food := foodBotFixture(t)
	service := derivedmutation.Service{DB: f.db, Food: food}
	source := foodMassageSource(t, f.db, "alice")
	command := legacyfood.Command{EventID: "food-bot", Name: "toggle_activity", Activity: "yoga", Key: "committed"}
	order, err := service.ExecuteFood(t.Context(), "alice", command, source)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
	require.NoError(t, err)
	replay, err := service.ExecuteFood(t.Context(), "alice", command, source)
	require.NoError(t, err)
	require.Equal(t, order.ID, replay.ID)
	require.Equal(t, order.Version, replay.Version)
	command.Key, command.OrderID, command.Version = "new", order.ID, order.Version
	_, err = service.ExecuteFood(t.Context(), "alice", command, source)
	requireCode(t, err, "source_stale")
	source = foodMassageSource(t, f.db, "alice")
	history := conversation.Service{DB: f.db}
	require.NoError(t, history.AppendOriginal(t.Context(), "alice", "source", "user", "original"))
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='source'`).Scan(&id),
	)
	require.NoError(t, history.DeleteContent(t.Context(), "alice", id))
	_, err = service.ExecuteFood(t.Context(), "alice", command, source)
	requireCode(t, err, "history_stale")
	_, err = food.Execute(t.Context(), "alice", command)
	require.NoError(t, err, "manual command is independent of model history")
}

func TestDerivedMassageSourceReceiptsAndPreferences(t *testing.T) {
	t.Parallel()
	db, domain, _ := massageFixture(t)
	service := derivedmutation.Service{DB: db, Massage: domain}
	source := foodMassageSource(t, db, "alice")
	command := massageBook("committed", "bob", 1, 2)
	booking, err := service.ExecuteMassage(t.Context(), "alice", command, source)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
	require.NoError(t, err)
	replay, err := service.ExecuteMassage(t.Context(), "alice", command, source)
	require.NoError(t, err)
	require.Equal(t, booking.ID, replay.ID)
	cancel := massage.Command{
		Event:   booking.Event,
		Booking: booking.ID,
		Version: booking.Version,
		Action:  "cancel",
		Key:     "cancel",
	}
	_, err = service.ExecuteMassage(t.Context(), "alice", cancel, source)
	requireCode(t, err, "source_stale")
	current, err := domain.Bookings(t.Context(), "alice", booking.Event, "", "mine")
	require.NoError(t, err)
	require.Len(t, current, 1)
	require.Nil(t, current[0].CancelledAt)
	_, err = domain.Execute(t.Context(), "alice", cancel)
	require.NoError(t, err)
	_, err = service.ExecuteMassage(t.Context(), "alice", cancel, source)
	require.NoError(t, err, "completed cancellation receipt remains replayable after source loss")
	_, err = db.Exec(t.Context(), `UPDATE core.massage_bookings SET owner='client' WHERE id=$1`, booking.ID)
	require.NoError(t, err)
	_, err = service.ExecuteMassage(t.Context(), "alice", cancel, source)
	requireCode(t, err, "not_found")
	staffSource := foodMassageSource(t, db, "bob")
	prefs := massage.Preferences{Bookings: false, Next: false}
	_, err = service.SetMassagePreferences(t.Context(), "bob", booking.Event, prefs, staffSource)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	_, err = service.SetMassagePreferences(
		t.Context(),
		"bob",
		booking.Event,
		massage.Preferences{Bookings: true},
		staffSource,
	)
	requireCode(t, err, "source_stale")
	stored, err := domain.Preferences(t.Context(), "bob", booking.Event)
	require.NoError(t, err)
	require.Equal(t, prefs, stored)
	_, err = service.SetMassagePreferences(t.Context(), "alice", booking.Event, prefs, source)
	requireCode(t, err, "forbidden")
}

func TestDerivedFoodRevocationAtEventBarrier(t *testing.T) {
	t.Parallel()
	f, food := foodBotFixture(t)
	service := derivedmutation.Service{DB: f.db, Food: food}
	source := foodMassageSource(t, f.db, "alice")
	barrier, err := f.db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(context.WithoutCancel(t.Context())) }()
	var pid int32
	require.NoError(t, barrier.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = barrier.Exec(t.Context(), `SELECT event_id FROM core.food_events WHERE event_id='food-bot' FOR UPDATE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, callErr := service.ExecuteFood(
			ctx,
			"alice",
			legacyfood.Command{EventID: "food-bot", Name: "toggle_activity", Activity: "yoga", Key: "blocked"},
			source,
		)
		done <- callErr
	}()
	waitMutationBlocked(t, f.db, pid, 1)
	_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
	require.NoError(t, err)
	require.NoError(t, barrier.Commit(ctx))
	requireCode(t, <-done, "source_stale")
	var count int
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM core.food_operations`).Scan(&count))
	require.Zero(t, count)
}

func TestDerivedMassagePreferencesRevocationAtTargetBarrier(t *testing.T) {
	t.Parallel()
	db, domain, _ := massageFixture(t)
	service := derivedmutation.Service{DB: db, Massage: domain}
	source := foodMassageSource(t, db, "bob")
	barrier, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(context.WithoutCancel(t.Context())) }()
	var pid int32
	require.NoError(t, barrier.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = barrier.Exec(t.Context(), `SELECT owner FROM core.massage_specialists WHERE owner='bob' FOR UPDATE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, callErr := service.SetMassagePreferences(ctx, "bob", "sandbox-festival", massage.Preferences{}, source)
		done <- callErr
	}()
	waitMutationBlocked(t, db, pid, 1)
	_, err = db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	require.NoError(t, barrier.Commit(ctx))
	requireCode(t, <-done, "source_stale")
	prefs, err := domain.Preferences(ctx, "bob", "sandbox-festival")
	require.NoError(t, err)
	require.True(t, prefs.Bookings)
}

func TestDerivedMassageInstantReceiptRequiresCurrentPractitioner(t *testing.T) {
	t.Parallel()
	db, domain, start := massageFixture(t)
	domain.Now = func() time.Time { return start.Add(time.Hour) }
	service := derivedmutation.Service{DB: db, Massage: domain}
	source := foodMassageSource(t, db, "bob")
	command := massage.Command{Event: "sandbox-festival", Party: "night", Action: "instant", Length: 1, Key: "instant"}
	_, err := service.ExecuteMassage(t.Context(), "bob", command, source)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.massage_notices; DELETE FROM core.massage_bookings WHERE specialist='bob'; DELETE FROM core.massage_work WHERE specialist='bob'; DELETE FROM core.massage_specialists WHERE owner='bob'`,
	)
	require.NoError(t, err)
	_, err = service.ExecuteMassage(t.Context(), "bob", command, source)
	requireCode(t, err, "forbidden")
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_operations WHERE actor='bob' AND key='instant'`).
			Scan(&count),
	)
	require.Equal(t, 1, count)
}

func TestDerivedFoodReviewReceiptRequiresCurrentGrant(t *testing.T) {
	t.Parallel()
	f, food, order := foodSubmittedFixture(t)
	service := derivedmutation.Service{DB: f.db, Food: food}
	source := foodMassageSource(t, f.db, "bob")
	command := legacyfood.Command{
		EventID:    "food-bot",
		OrderID:    order.ID,
		Version:    order.Version,
		Name:       "accept",
		Kind:       legacyfood.Activity,
		Generation: order.ActivityPayment.Generation,
		Key:        "accept",
	}
	_, err := service.ExecuteFood(t.Context(), "bob", command, source)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.food_admins SET can_review=false WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.ExecuteFood(t.Context(), "bob", command, source)
	requireCode(t, err, "forbidden")
}

func TestDerivedMassageSourceGrantHeldThroughCommit(t *testing.T) {
	t.Parallel()
	db, domain, _ := massageFixture(t)
	service := derivedmutation.Service{DB: db, Massage: domain}
	source := foodMassageSource(t, db, "alice")
	barrier, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(context.WithoutCancel(t.Context())) }()
	var pid int32
	require.NoError(t, barrier.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = barrier.Exec(t.Context(), `SELECT id FROM core.massage_parties WHERE id='night' FOR UPDATE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	effect := make(chan error, 1)
	go func() {
		_, callErr := service.ExecuteMassage(ctx, "alice", massageBook("grant-lock", "bob", 1, 2), source)
		effect <- callErr
	}()
	waitMutationBlocked(t, db, pid, 1)
	revoke := make(chan error, 1)
	go func() {
		_, deleteErr := db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
		revoke <- deleteErr
	}()
	waitMutationBlocked(t, db, pid, 2)
	require.NoError(t, barrier.Commit(ctx))
	require.NoError(t, <-effect)
	require.NoError(t, <-revoke)
	var count int
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT count(*) FROM core.massage_operations WHERE key='grant-lock'`).Scan(&count),
	)
	require.Equal(t, 1, count)
}
