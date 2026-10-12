package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/shuttle/internal/db"
)

func testStore(t *testing.T) *db.Store {
	t.Helper()
	dsn := os.Getenv("SHUTTLE_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("SHUTTLE_TEST_DATABASE_DSN")
	}
	if dsn == "" {
		t.Skip("SHUTTLE_TEST_DATABASE_URL is required for real PostgreSQL tests")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	parsed, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	s, e := db.Open(ctx, parsed.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close(); _, _ = p.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); p.Close() })
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	return s
}
func accepted(t *testing.T, s *db.Store, update int64) db.Task {
	t.Helper()
	v, _, e := s.Intake(
		context.Background(),
		db.IntakeRequest{
			BotID:          1,
			UpdateID:       update,
			TelegramUserID: 2,
			ChatID:         3,
			Payload:        json.RawMessage(`{"kind":"message","text":"hello"}`),
		},
		nil,
	)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestIntakeDuplicateAndRollback(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	var mu sync.Mutex
	calls := 0
	enqueue := func(ctx context.Context, _ pgx.Tx, _ uuid.UUID) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return ctx.Err()
	}
	r := db.IntakeRequest{
		BotID:          1,
		UpdateID:       1,
		TelegramUserID: 2,
		ChatID:         3,
		Payload:        json.RawMessage(`{"kind":"message","text":"first"}`),
	}
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 8)
	for range 8 {
		wg.Go(func() {
			v, _, e := s.Intake(ctx, r, enqueue)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- v.ID
		})
	}
	wg.Wait()
	close(ids)
	var id uuid.UUID
	for got := range ids {
		if id == uuid.Nil {
			id = got
		}
		if got != id {
			t.Fatal("duplicate task")
		}
	}
	if calls != 1 {
		t.Fatalf("enqueue count %d", calls)
	}
	r.Payload = json.RawMessage(`{"kind":"message","text":"changed"}`)
	v, dup, e := s.Intake(ctx, r, enqueue)
	if e != nil || !dup || !strings.Contains(string(v.Request), "first") {
		t.Fatalf("duplicate mutation: %+v %v", v, e)
	}
	r.UpdateID = 2
	_, _, e = s.Intake(
		ctx,
		r,
		func(context.Context, pgx.Tx, uuid.UUID) error { return errors.New("queue unavailable") },
	)
	if e == nil {
		t.Fatal("enqueue failure ignored")
	}
	var count int
	if e = s.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_inbox WHERE update_id=2").
		Scan(&count); e != nil ||
		count != 0 {
		t.Fatal("intake survived rollback", e)
	}
}
func TestRoutingControlAndFence(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	a := accepted(t, s, 1)
	a, e := s.Route(ctx, a.ID)
	if e != nil || a.Route != "new_run" {
		t.Fatal(a, e)
	}
	a, e = s.Claim(ctx, a.ID, "worker-a", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	a.State = "awaiting_user"
	a.RunID = "run-1"
	a.QuestionID = "q1"
	a, e = s.Save(ctx, a, "worker-a", a.Fence, nil)
	if e != nil {
		t.Fatal(e)
	}
	b := accepted(t, s, 2)
	b, e = s.Route(ctx, b.ID)
	if e != nil || b.Route != "input" || b.TargetTaskID != a.ID || b.QuestionID != "q1" {
		t.Fatal(b, e)
	}
	cancel, _, e := s.Intake(
		ctx,
		db.IntakeRequest{
			BotID:          1,
			UpdateID:       3,
			TelegramUserID: 2,
			ChatID:         3,
			Payload:        json.RawMessage(`{"kind":"cancel"}`),
		},
		nil,
	)
	if e != nil {
		t.Fatal(e)
	}
	cancel, e = s.Route(ctx, cancel.ID)
	if e != nil || cancel.Route != "cancel" || cancel.TargetTaskID != a.ID {
		t.Fatal(cancel, e)
	}
	if e = s.Release(ctx, a.ID, "worker-a", a.Fence); e != nil {
		t.Fatal(e)
	}
	next, e := s.Claim(ctx, a.ID, "worker-b", time.Minute)
	if e != nil || next.Fence <= a.Fence {
		t.Fatal(next, e)
	}
	a.State = "completed"
	if _, e = s.Save(ctx, a, "worker-a", a.Fence, nil); !errors.Is(e, db.ErrLeaseLost) {
		t.Fatal("stale writer accepted", e)
	}
}
func TestSettingsCAS(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	settings, e := s.Settings(ctx)
	if e != nil {
		t.Fatal(e)
	}
	settings.Document.DefaultLanguage = "ru"
	next, e := s.CompareAndSwapSettings(ctx, settings.Version, settings.Document, "admin")
	if e != nil || next.Version != settings.Version+1 {
		t.Fatal(next, e)
	}
	if _, e = s.CompareAndSwapSettings(
		ctx,
		settings.Version,
		settings.Document,
		"admin",
	); !errors.Is(
		e,
		db.ErrConflict,
	) {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	settings, e = s.Settings(ctx)
	if e != nil || settings.Document.DefaultLanguage != "ru" {
		t.Fatal("seed overwrote settings", e)
	}
}

func TestOutboxUnknown(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	var e error
	a := accepted(t, s, 1)
	a, e = s.Claim(ctx, a.ID, "w", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	out := db.Outbound{
		DedupKey: a.ID.String() + ":reply:0",
		ChunkSeq: 0,
		Payload:  json.RawMessage(`{"chat_id":3,"text":"reply"}`),
	}
	a, e = s.Save(ctx, a, "w", a.Fence, []db.Outbound{out})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Save(ctx, a, "w", a.Fence, []db.Outbound{out})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := s.ListOutbox(ctx, a.ID)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	d, e := s.ReserveDelivery(ctx, rows[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveDelivery(ctx, d.ID); e == nil {
		t.Fatal("second send reservation accepted")
	}
	if _, e = s.RecoverUnknownDeliveries(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveDelivery(ctx, d.ID); e == nil {
		t.Fatal("unknown delivery replayed")
	}
	rows, e = s.ListOutbox(ctx, a.ID)
	if e != nil || rows[0].State != "delivery_unknown" {
		t.Fatal(rows, e)
	}
}

func TestRoutingBeforeRunAndNoActive(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	c, _, err := s.Intake(
		ctx,
		db.IntakeRequest{
			BotID:          1,
			UpdateID:       1,
			TelegramUserID: 2,
			ChatID:         3,
			Payload:        json.RawMessage(`{"kind":"cancel"}`),
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Route(ctx, c.ID)
	if err != nil || c.Route != "no_active" {
		t.Fatal(c, err)
	}
	first := accepted(t, s, 2)
	second := accepted(t, s, 3)
	if _, err = s.Route(ctx, second.ID); !errors.Is(err, db.ErrRoutingPending) {
		t.Fatal("later inbox overtook head", err)
	}
	first, err = s.Route(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err = s.Route(ctx, second.ID)
	if err != nil || second.Route != "input" || second.TargetTaskID != first.ID || second.TargetRunID != "" {
		t.Fatal("new run duplicated during creation", second, err)
	}
}

func TestTerminalDeliveryReleasesConversation(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"delivery_unknown", "delivery_failed"} {
		t.Run(outcome, func(t *testing.T) { t.Parallel(); checkTerminalDelivery(t, outcome) })
	}
}
func checkTerminalDelivery(t *testing.T, outcome string) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	first := accepted(t, s, 1)
	var err error
	first, err = s.Route(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err = s.Claim(ctx, first.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first.State = outcome
	outbound := db.Outbound{
		DedupKey: first.ID.String() + ":reply:0",
		Payload:  json.RawMessage(`{"chat_id":3,"text":"reply"}`),
	}
	if _, err = s.Save(ctx, first, "worker", first.Fence, []db.Outbound{outbound}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListOutbox(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := s.ReserveDelivery(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishDelivery(ctx, reservation.ID, reservation.AttemptID, outcome, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveDelivery(ctx, reservation.ID); err == nil {
		t.Fatal("terminal delivery replayed")
	}
	next := accepted(t, s, 2)
	next, err = s.Route(ctx, next.ID)
	if err != nil || next.Route != "new_run" {
		t.Fatal("terminal delivery held conversation", next, err)
	}
}

func TestControlReadyOrdersOnlyControls(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	primary := accepted(t, s, 1)
	var err error
	_, err = s.Route(ctx, primary.ID)
	if err != nil {
		t.Fatal(err)
	}
	earlier := accepted(t, s, 2)
	earlier, err = s.Route(ctx, earlier.ID)
	if err != nil {
		t.Fatal(err)
	}
	later := accepted(t, s, 3)
	later, err = s.Route(ctx, later.ID)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := s.ControlReady(ctx, earlier.ID)
	if err != nil || !ready {
		t.Fatal("primary run blocked first control", err)
	}
	ready, err = s.ControlReady(ctx, later.ID)
	if err != nil || ready {
		t.Fatal("later control overtook earlier input", err)
	}
	earlier, err = s.Claim(ctx, earlier.ID, "control-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	earlier.State = "completed"
	if _, err = s.Save(ctx, earlier, "control-worker", earlier.Fence, nil); err != nil {
		t.Fatal(err)
	}
	ready, err = s.ControlReady(ctx, later.ID)
	if err != nil || !ready {
		t.Fatal("terminal earlier control blocked later", err)
	}
}

func TestBlockedDeliverySuffixCannotReserve(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	task := accepted(t, s, 1)
	var err error
	task, err = s.Claim(ctx, task.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	chunks := []db.Outbound{
		{
			DedupKey: task.ID.String() + ":reply:0",
			ChunkSeq: 0,
			Payload:  json.RawMessage(`{"chat_id":3,"text":"first"}`),
		},
		{
			DedupKey: task.ID.String() + ":reply:1",
			ChunkSeq: 1,
			Payload:  json.RawMessage(`{"chat_id":3,"text":"second"}`),
		},
	}
	if _, err = s.Save(ctx, task, "worker", task.Fence, chunks); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListOutbox(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := s.ReserveDelivery(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishDelivery(ctx, reservation.ID, reservation.AttemptID, "delivery_unknown", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.BlockDeliverySuffix(ctx, task.ID, reservation.ChunkSeq); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListOutbox(ctx, task.ID)
	if err != nil || rows[1].State != "delivery_blocked" {
		t.Fatal("suffix remained pending", rows, err)
	}
	if _, err = s.ReserveDelivery(ctx, rows[1].ID); err == nil {
		t.Fatal("blocked suffix reserved")
	}
}

func routedInput(t *testing.T, s *db.Store) (db.Task, db.Task) {
	t.Helper()
	ctx := context.Background()
	primary := accepted(t, s, 1)
	var err error
	primary, err = s.Route(ctx, primary.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := accepted(t, s, 2)
	input, err = s.Route(ctx, input.ID)
	if err != nil {
		t.Fatal(err)
	}
	return primary, input
}

func completePrimary(t *testing.T, s *db.Store, primary db.Task) {
	t.Helper()
	ctx := context.Background()
	primary, err := s.Claim(ctx, primary.ID, "primary-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	primary.State = "completed"
	if _, err = s.Save(ctx, primary, "primary-worker", primary.Fence, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteMessagePreservesAcceptedRequest(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	primary, input := routedInput(t, s)
	input, err := s.Claim(ctx, input.ID, "input-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromoteMessage(ctx, input.ID, "input-worker", input.Fence); !errors.Is(err, db.ErrRoutingPending) {
		t.Fatal("running primary replaced", err)
	}
	completePrimary(t, s, primary)
	promoted, err := s.PromoteMessage(ctx, input.ID, "input-worker", input.Fence)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ID != input.ID || promoted.Seq != input.Seq || string(promoted.Request) != string(input.Request) {
		t.Fatal("accepted request changed", promoted)
	}
	if promoted.Route != "new_run" || promoted.TargetTaskID != uuid.Nil || promoted.TargetRunID != "" ||
		promoted.QuestionID != "" {
		t.Fatal("control association survived promotion", promoted)
	}
	next := accepted(t, s, 3)
	next, err = s.Route(ctx, next.ID)
	if err != nil || next.TargetTaskID != promoted.ID {
		t.Fatal("promoted task not active", next, err)
	}
}

func TestPromoteMessageCannotReplaceAnotherActiveTask(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	primary, input := routedInput(t, s)
	other := accepted(t, s, 3)
	var err error
	other, err = s.Route(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	completePrimary(t, s, primary)
	input, err = s.Claim(ctx, input.ID, "input-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromoteMessage(ctx, input.ID, "input-worker", input.Fence); err != nil {
		t.Fatal(err)
	}
	other, err = s.Claim(ctx, other.ID, "other-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromoteMessage(ctx, other.ID, "other-worker", other.Fence); !errors.Is(err, db.ErrRoutingPending) {
		t.Fatal("another active task replaced", err)
	}
}
func TestPromoteMessageRejectsStaleFence(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	primary, input := routedInput(t, s)
	completePrimary(t, s, primary)
	input, err := s.Claim(ctx, input.ID, "old-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Release(ctx, input.ID, "old-worker", input.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx, input.ID, "new-worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromoteMessage(ctx, input.ID, "old-worker", input.Fence); !errors.Is(err, db.ErrLeaseLost) {
		t.Fatal("stale worker promoted message", err)
	}
}

func TestRoutingWaitsForEarlierMessagePromotion(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	primary, input := routedInput(t, s)
	completePrimary(t, s, primary)
	later := accepted(t, s, 3)
	if _, err := s.Route(ctx, later.ID); !errors.Is(err, db.ErrRoutingPending) {
		t.Fatal("later message overtook promotable input", err)
	}
	input, err := s.Claim(ctx, input.ID, "input-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromoteMessage(ctx, input.ID, "input-worker", input.Fence); err != nil {
		t.Fatal(err)
	}
	later, err = s.Route(ctx, later.ID)
	if err != nil || later.Route != "input" || later.TargetTaskID != input.ID {
		t.Fatal("later message did not route to promoted task", later, err)
	}
}
