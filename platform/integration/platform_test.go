// Package integration tests HTTP adapters and PostgreSQL transactions together.
package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("set TEST_DATABASE_URL to a disposable PostgreSQL cluster")
	}
	admin, e := pgxpool.New(t.Context(), url)
	require.NoError(t, e)

	t.Cleanup(admin.Close)
	suffix := make([]byte, 8)
	{
		_, e = rand.Read(suffix)
		require.NoError(t, e)
	}

	name := "synthetic_qa_zns_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	{
		_, e = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
		require.NoError(t, e)
	}

	cfg, e := pgxpool.ParseConfig(url)
	require.NoError(t, e)

	cfg.ConnConfig.Database = name
	p, e := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, e)

	t.Cleanup(func() {
		p.Close()
		{
			_, e = admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
			assert.NoError(t, e)
		}
	})
	{
		e = store.Migrate(t.Context(), p)
		require.NoError(t, e)
	}
	{
		e = store.Seed(t.Context(), p)
		require.NoError(t, e)
	}

	return p
}
func action(name, slot string, version int64, key, origin string) workflow.Action {
	return workflow.Action{Name: name, SlotID: slot, Version: version, Key: key, Origin: origin}
}
func requireCode(t *testing.T, e error, code string) {
	t.Helper()
	var p *core.ProblemError
	if !errors.As(e, &p) || p.Code != code {
		t.Fatalf("want %s; got %v", code, e)
	}
}
func mustExec(t *testing.T, s workflow.Service, owner string, a workflow.Action) workflow.Workflow {
	t.Helper()
	w, e := s.Execute(t.Context(), owner, a)
	require.NoError(t, e)

	return w
}

func TestCoreCapacityReplayAndRelease(t *testing.T) {
	t.Parallel()
	p := database(t)
	s := workflow.Service{DB: p}
	for _, owner := range []string{"alice", "bob"} {
		mustExec(t, s, owner, action("select", "massage-1", 0, "select", "manual"))
	}
	start := make(chan struct{})
	results := make(chan string, 2)
	var wg sync.WaitGroup
	for _, owner := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			<-start
			_, e := s.Execute(t.Context(), owner, action("confirm", "", 1, "confirm", "manual"))
			if e == nil {
				results <- owner
			} else {
				var p *core.ProblemError
				if !errors.As(e, &p) || p.Code != "sold_out" {
					t.Errorf("unexpected error %v", e)
				}
				results <- ""
			}
		}(owner)
	}
	close(start)
	wg.Wait()
	close(results)
	winner := ""
	count := 0
	for owner := range results {
		if owner != "" {
			winner = owner
			count++
		}
	}
	if count != 1 {
		t.Fatalf("bookings: %d", count)
	}
	replay := mustExec(t, s, winner, action("confirm", "", 1, "confirm", "manual"))
	if replay.Version != 2 {
		t.Fatal(replay)
	}
	_, e := s.Execute(t.Context(), winner, action("cancel", "", 2, "confirm", "manual"))
	requireCode(t, e, "idempotency_conflict")
	_, e = s.Execute(t.Context(), winner, action("cancel", "", 1, "stale", "manual"))
	requireCode(t, e, "stale_view")
	mustExec(t, s, winner, action("cancel", "", 2, "cancel", "manual"))
	loser := "bob"
	if winner == "bob" {
		loser = "alice"
	}
	mustExec(t, s, loser, action("confirm", "", 1, "retry-after-free", "manual"))
	var n int
	if e = p.QueryRow(t.Context(), `SELECT count(*) FROM core.workflows WHERE state='booked'`).
		Scan(&n); e != nil ||
		n != 1 {
		t.Fatalf("%d %v", n, e)
	}
}

func TestPolicyExpiryAndRevocation(t *testing.T) {
	t.Parallel()
	p := database(t)
	s := workflow.Service{DB: p}
	for _, origin := range []string{"manual", "agent"} {
		_, e := s.Execute(t.Context(), "visitor", action("select", "massage-1", 0, origin, origin))
		requireCode(t, e, "forbidden")
	}
	mustExec(t, s, "alice", action("select", "massage-1", 0, "select", "agent"))
	_, e := s.Execute(t.Context(), "alice", action("confirm", "", 1, "model-confirm", "agent"))
	requireCode(t, e, "human_confirmation_required")
	{
		_, e = p.Exec(
			t.Context(),
			`UPDATE core.workflows SET expires_at=now()-interval '1 second' WHERE owner='alice'`,
		)
		require.NoError(t, e)
	}

	_, e = s.Execute(t.Context(), "alice", action("confirm", "", 1, "expired", "manual"))
	requireCode(t, e, "intent_expired")
	mustExec(t, s, "alice", action("select", "massage-1", 1, "renew", "manual"))
	{
		_, e = p.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
		require.NoError(t, e)
	}

	_, e = s.Execute(t.Context(), "alice", action("confirm", "", 2, "revoked", "manual"))
	requireCode(t, e, "forbidden")
	_, e = s.Execute(t.Context(), "alice", action("select", "massage-1", 1, "renew", "manual"))
	requireCode(t, e, "forbidden")
}

func TestAPITrustBoundary(t *testing.T) {
	t.Parallel()
	p := database(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	server := httptest.NewServer(
		api.Handler(notificationFixtureServices(p, appservices.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	defer server.Close()
	for _, tc := range []struct {
		token, body string
		status      int
	}{{"", `{}`, 401}, {signer.Token("unknown"), `{}`, 403}, {signer.Token("alice"), `{"name":"select","slot_id":"massage-1","version":0,"key":"x","origin":"manual","owner":"bob"}`, 400}, {signer.Token("alice"), `{} {}`, 400}} {
		r, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/actions", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		resp, e := http.DefaultClient.Do(r)
		require.NoError(t, e)

		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("want %d got %d", tc.status, resp.StatusCode)
		}
	}
	c := appclient.Client{Base: server.URL, SandboxToken: signer.Token}
	{
		_, e := c.Execute(t.Context(), "alice", action("select", "massage-1", 0, "x", "manual"))
		require.NoError(t, e)
	}

	w, e := c.Current(t.Context(), "bob")
	if e != nil || w.State != "empty" {
		t.Fatalf("cross-user state: %v %v", w, e)
	}
}

type recordingModel struct {
	input agent.Input
	plan  agent.Plan
	calls int
}

func (m *recordingModel) Plan(_ context.Context, in agent.Input) (agent.Plan, error) {
	m.input = in
	m.calls++
	return m.plan, nil
}

type fixture struct {
	db    *pgxpool.Pool
	b     *bot.Bot
	fake  *httptest.Server
	model *recordingModel
}

func setup(t *testing.T) *fixture {
	p := database(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	as := httptest.NewServer(
		api.Handler(notificationFixtureServices(p, appservices.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	t.Cleanup(as.Close)
	f, e := sandbox.New(t.Context(), p, "sandbox")
	require.NoError(t, e)

	ts := httptest.NewServer(f.Handler())
	t.Cleanup(ts.Close)
	m := &recordingModel{plan: agent.Plan{Text: "Вижу выбор; подтвердите кнопкой.", View: "workflow"}}
	result := &fixture{
		p,
		&bot.Bot{
			Delivery: syntheticDeliverySettings(),
			DB:       p,
			API:      appclient.Client{Base: as.URL, SandboxToken: signer.Token},
			TG:       telegram.Client{Base: ts.URL, Token: "sandbox"},
			Model:    m,
		},
		ts,
		m,
	}
	result.b.Host = appclient.Host{
		Base:   as.URL,
		Signer: signer,
		UserToken: func(ctx context.Context, owner string) (string, error) {
			return result.b.API.UserToken(ctx, owner)
		},
	}
	return result
}
func message(id, user int64, text string) telegram.Update {
	return telegram.Update{
		ID: id,
		Message: &telegram.Message{
			ID:   id,
			Text: text,
			From: telegram.User{ID: user},
			Chat: telegram.Chat{ID: user, Type: "private"},
		},
	}
}
func aliceCallback(id, mid int64, data string) telegram.Update {
	return telegram.Update{
		ID: id,
		Callback: &telegram.Callback{
			ID:      strconv.FormatInt(id, 10),
			Data:    data,
			From:    telegram.User{ID: identity.AliceTelegramID},
			Message: telegram.Message{ID: mid, Chat: telegram.Chat{ID: identity.AliceTelegramID, Type: "private"}},
		},
	}
}
func handle(t *testing.T, b *bot.Bot, u telegram.Update) {
	t.Helper()
	{
		e := b.Handle(t.Context(), u)
		require.NoError(t, e)
	}
}
func (tst *fixture) aliceCard(t *testing.T) int64 {
	t.Helper()
	var id int64
	{
		e := tst.db.QueryRow(t.Context(), `SELECT message_id FROM bot.messages WHERE owner=$1`, "alice").Scan(&id)
		require.NoError(t, e)
	}

	return id
}
func post(t *testing.T, url string, body any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	r, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Sandbox", "1")
	res, e := http.DefaultClient.Do(r)
	require.NoError(t, e)

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: %d", url, res.StatusCode)
	}
}
func TestManualAgentContinuationAndStaleButtons(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(100, 101, "/start"))
	mid := f.aliceCard(t)
	handleVisible(t, f.b, aliceCallback(101, mid, "select:massage-1:0"))
	handleVisible(t, f.b, message(102, 101, "помоги закончить"))
	if f.model.input.Workflow.State != "draft" {
		t.Fatal("agent missing draft")
	}
	found := false
	for _, v := range f.model.input.History {
		if v.Kind == "result" && strings.Contains(string(v.Content), "select") {
			found = true
		}
	}
	if !found {
		t.Fatal("agent missing manual history")
	}
	handleVisible(t, f.b, aliceCallback(103, mid, "confirm::1"))
	handleVisible(t, f.b, aliceCallback(103, mid, "confirm::1"))
	w, e := f.b.API.Current(t.Context(), "alice")
	if e != nil || w.State != "booked" || w.Version != 2 {
		t.Fatalf("%v %v", w, e)
	}
	if f.aliceCard(t) != mid {
		t.Fatal("did not update original GUI")
	}
	handleVisible(t, f.b, aliceCallback(104, mid, "cancel::1"))
	w, _ = f.b.API.Current(t.Context(), "alice")
	if w.State != "booked" {
		t.Fatal("stale cancellation accepted")
	}
	handleVisible(t, f.b, message(105, 202, "hello"))
	if f.model.input.Workflow.State != "empty" {
		t.Fatal("cross user workflow")
	}
	for _, v := range f.model.input.History {
		if strings.Contains(string(v.Content), "massage-1") {
			t.Fatal("cross user history")
		}
	}
}
func TestAgentProposalRetryAndHostileModel(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.model.plan.Action = &agent.Proposal{Name: "select", SlotID: "shuttle-1"}
	u := message(201, 101, "выбери трансфер")
	handleVisible(t, f.b, u)
	handleVisible(t, f.b, u)
	if f.model.calls != 1 {
		t.Fatal("retry re-ran model")
	}
	w, _ := f.b.API.Current(t.Context(), "alice")
	if w.Version != 1 {
		t.Fatal("retry mutated twice")
	}
	f.model.plan.Action = &agent.Proposal{Name: "confirm"}
	handleVisible(t, f.b, message(202, 101, "confirm now"))
	w, _ = f.b.API.Current(t.Context(), "alice")
	if w.State != "draft" {
		t.Fatal("model confirmed a write")
	}
	handleVisible(t, f.b, aliceCallback(203, f.aliceCard(t), "confirm::1"))
	w, _ = f.b.API.Current(t.Context(), "alice")
	if w.State != "booked" {
		t.Fatal("manual blocked by model failure")
	}
}
func TestTelegramFailureAndPersistence(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Delivery.Fallback = time.Second
	handleVisible(t, f.b, message(301, 101, "/start"))
	old := f.aliceCard(t)
	post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
	u := aliceCallback(302, old, "select:massage-1:0")
	require.NoError(t, f.b.Handle(t.Context(), u))
	failed := assertBotRateLimited(t, f)
	require.NoError(t, f.b.Handle(t.Context(), u))
	waitBotRetryDeadline(t, f, failed)
	pumpBotDeliveries(t, f.b)
	w, _ := f.b.API.Current(t.Context(), "alice")
	if w.Version != 1 {
		t.Fatal("retry wrote twice")
	}
	post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "edit_missing"})
	handleVisible(t, f.b, message(303, 101, "help"))
	var replacementReady time.Time
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT not_before FROM bot.delivery_intents
 WHERE owner='alice' AND state='pending' AND reason='edit_target_missing'`).Scan(&replacementReady))
	require.Eventually(t, func() bool {
		var ready bool
		err := f.db.QueryRow(t.Context(), `SELECT clock_timestamp()>=$1`, replacementReady).Scan(&ready)
		return err == nil && ready
	}, 5*time.Second, 10*time.Millisecond)
	pumpBotDeliveries(t, f.b)
	if f.aliceCard(t) == old {
		t.Fatal("missing edit not replaced")
	}
	restored, e := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, e)

	server := httptest.NewServer(restored.Handler())
	defer server.Close()
	oldClient := f.b.TG
	f.b.TG.Base = server.URL
	handleVisible(t, f.b, aliceCallback(304, f.aliceCard(t), "confirm::1"))
	f.b.TG = oldClient
	var n int
	if e = f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.audit WHERE owner='alice'`).
		Scan(&n); e != nil ||
		n != 2 {
		t.Fatalf("audit %d %v", n, e)
	}
}

func TestPollerEndToEnd(t *testing.T) {
	t.Parallel()
	f := setup(t)
	c, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(c) }()
	t.Cleanup(func() {
		cancel()
		{
			e := <-done
			assert.NoError(t, e)
		}
	})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "/start"})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		{
			e := f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.messages`).Scan(&n)
			require.NoError(t, e)
		}

		if n == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("poller did not render")
}

func TestDeniedProposalDoesNotClaimSuccessAndStartReflectsState(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.model.plan = agent.Plan{
		Text:   "Подготовил выбранную услугу",
		View:   "workflow",
		Action: &agent.Proposal{Name: "select", SlotID: "massage-1"},
	}
	handleVisible(t, f.b, message(401, 303, "выбери массаж"))
	cardText := func(user string) string {
		resp, e := http.Get(f.fake.URL + "/lab/state?user=" + user)
		require.NoError(t, e)

		defer resp.Body.Close()
		var s liveState
		{
			e = json.NewDecoder(resp.Body).Decode(&s)
			require.NoError(t, e)
		}

		if len(s.Messages) == 0 {
			t.Fatal("missing card")
		}
		return s.Messages[len(s.Messages)-1].Text
	}
	text := cardText("303")
	if strings.Contains(text, "Подготовил") || !strings.Contains(text, "forbidden") {
		t.Fatal(text)
	}
	handleVisible(t, f.b, message(402, 101, "/start"))
	mid := f.aliceCard(t)
	handleVisible(t, f.b, aliceCallback(403, mid, "select:massage-1:0"))
	handleVisible(t, f.b, aliceCallback(404, mid, "confirm::1"))
	handleVisible(t, f.b, message(405, 101, "/start"))
	text = cardText("101")
	if strings.Contains(text, "Выберите услугу") || !strings.Contains(text, "Бронирование оформлено") {
		t.Fatal(text)
	}
}
