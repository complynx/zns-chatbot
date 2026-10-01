package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestPassIndependentSourceBudgetsAdmissionAndRetirement(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"admission_valid", "preparation_revoked_prior", "receipt_revoked_prior"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runPassIndependentSourceBudgets(t, scenario)
		})
	}
}

func runPassIndependentSourceBudgets(t *testing.T, scenario string) {
	t.Helper()
	f := passMenuFixture(t)
	priorRun := runPassVM(t, f, 73200, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, priorRun.Error)
	prior := widenPendingPassSource(t, f, "prior-budget")
	pumpBotDeliveries(t, f.b)
	prior, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, prior.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, prior.State)
	require.True(t, prior.ContinuationDone)
	require.NotEmpty(t, prior.Receipt.Tokens)
	if scenario != "admission_valid" {
		deletePassDeliveryHistory(t, f, "alice")
	}
	currentRun := runPassVM(t, f, 73201, 101, "Show my pass again",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, currentRun.Error)
	current := widenPendingPassSource(t, f, "current-budget")
	require.Equal(t, prior.MessageID, current.Target)
	require.False(t, readsource.Valid(append(readsource.CloneAuthorities(prior.Reference.Source.Authorities),
		current.Reference.Source.Authorities...)), "the transient union exceeds one persisted-record budget")
	changed, refused := false, false
	if scenario == "preparation_revoked_prior" {
		_, err = f.db.Exec(t.Context(), `INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice');
 UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`)
		require.NoError(t, err)
		f.b.Host.HTTP = &http.Client{
			Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
				if !changed && request.URL.Path == "/internal/bot-delivery/begin" {
					_, revokeErr := f.db.Exec(
						request.Context(),
						`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
					)
					if revokeErr != nil {
						return nil, revokeErr
					}
					changed = true
				}
				return http.DefaultTransport.RoundTrip(request)
			}),
		}
	}
	if scenario == "receipt_revoked_prior" {
		f.b.TG.HTTP = &http.Client{
			Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
				if !refused && strings.HasSuffix(request.URL.Path, "/editMessageText") {
					refused = true
					return passRefusedEditResponse(request), nil
				}
				return http.DefaultTransport.RoundTrip(request)
			}),
		}
	}
	pumpBotDeliveries(t, f.b)
	pumpBotDeliveries(t, f.b)
	assertPassIndependentSourceResult(t, f, scenario, current, prior, changed, refused)
}

func assertPassIndependentSourceResult(t *testing.T, f *fixture, scenario string,
	current, prior botdelivery.Intent, changed, refused bool,
) {
	t.Helper()
	actual, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, current.QueueReference(), false)
	require.NoError(t, err)
	if scenario == "preparation_revoked_prior" {
		require.True(t, changed, "withdraw capability after rendering and before admission")
		require.Equal(t, delivery.Cancelled, actual.State)
		require.Zero(t, actual.Attempt)
	} else {
		require.Equal(t, delivery.Succeeded, actual.State)
		require.True(
			t,
			actual.ContinuationDone,
			"valid current receipt must not remain blocked by the prior record's union",
		)
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), current.QueueReference()))
		require.NotEmpty(t, passMenuCardOrEmpty(t, f, actual.MessageID).Markup.Rows)
		if scenario == "receipt_revoked_prior" {
			require.True(t, refused)
			require.NotEqual(t, prior.MessageID, actual.MessageID)
		} else {
			require.Equal(t, prior.MessageID, actual.MessageID)
		}
	}
	var cleanup, oldCallbacks int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE reference->>'family'=$1 AND target_message_id=$2 AND state='sent'`,
		botdelivery.PassReceiptRedactionFamily, prior.MessageID).Scan(&cleanup))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons
 WHERE owner='alice' AND revision=$1 AND token=ANY($2)`, prior.Receipt.Revision, prior.Receipt.Tokens).Scan(&oldCallbacks))
	if scenario == "admission_valid" {
		require.Zero(t, cleanup)
	} else {
		require.Equal(t, 1, cleanup)
		require.Zero(t, oldCallbacks)
		retired := passMenuCardOrEmpty(t, f, prior.MessageID)
		require.Equal(t, prior.MessageID, retired.ID)
		require.Empty(t, retired.Markup.Rows)
		require.Contains(t, retired.Text, "/passes")
	}
}

// Supply trusted fixture read evidence before the producer materializes a card.
// No successful receipt is rewritten to manufacture predecessor compatibility.
func widenPendingPassSource(t *testing.T, f *fixture, prefix string) botdelivery.Intent {
	t.Helper()
	i := pendingPassReceipt(t, f)
	require.NotNil(t, i.Reference.Source)
	refs := make([]readsource.Authority, 129)
	events := make([]string, len(refs))
	for index := range refs {
		events[index] = fmt.Sprintf("%s-%03d", prefix, index)
		refs[index].Registration = passbooking.ReadAuthority{Kind: passbooking.ReadOwnerMenu, Event: events[index]}
	}
	require.True(t, readsource.Valid(refs))
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at)
 SELECT unnest($1::text[]),clock_timestamp()+interval '1 year'`, events)
	require.NoError(t, err)
	i.Reference.Source.Authorities = refs
	require.True(t, i.Reference.Valid(i.Owner))
	reference, err := json.Marshal(i.Reference)
	require.NoError(t, err)
	source, err := json.Marshal(i.Reference.Source)
	require.NoError(t, err)
	updated, err := f.db.Exec(t.Context(), `UPDATE bot.delivery_intents SET reference=$4
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND state='pending' AND attempt=0`,
		i.BotID, i.Operation, i.Effect, reference)
	require.NoError(t, err)
	require.EqualValues(t, 1, updated.RowsAffected())
	_, err = f.db.Exec(t.Context(), `UPDATE bot.pass_views SET state=jsonb_set(state,'{source}',$1::jsonb)
 WHERE owner='alice' AND revision=$2`, source, i.Reference.Revision)
	require.NoError(t, err)
	return i
}

func TestPassCleanupPreservesNewerSameTargetManualPayload(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"manual", "home"} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			runPassCleanupNewerPayload(t, origin)
		})
	}
}

func TestPassCleanupRechecksConcurrentManualFinishAfterLaneLock(t *testing.T) {
	t.Parallel()
	runPassCleanupNewerPayload(t, "concurrent_manual")
}

func TestPassCleanupPreservesParentAfterCapabilityRegrant(t *testing.T) {
	t.Parallel()
	runPassCleanupNewerPayload(t, "regrant")
}

func runPassCleanupNewerPayload(t *testing.T, origin string) {
	t.Helper()
	f := passMenuFixture(t)
	handlePassVisible(t, f, message(73100, 101, "/passes"))
	target := passMenuCard(t, f, 101).ID
	run := runPassVM(t, f, 73101, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, run.Error)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice');
 UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`)
	require.NoError(t, err)
	original := pendingPassReceipt(t, f)
	drainOtherPassFixtureIntents(t, f)
	f.b.Host.HTTP = &http.Client{Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/internal/bot-delivery/receipt" {
			return nil, io.EOF
		}
		return http.DefaultTransport.RoundTrip(request)
	})}
	require.Error(t, f.b.DeliverBotIntent(t.Context(), original.QueueReference()))
	f.b.Host.HTTP = nil
	sent, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, original.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, sent.State)
	require.False(t, sent.ContinuationDone)
	require.Equal(t, target, sent.MessageID)
	require.Equal(t, "proof_accept", sent.Receipt.Pass.Capability)
	if origin == "home" {
		newRun := runPassVM(t, f, 73102, 101, "Show my pass independently",
			`return tools.passes.registration.show({event:"dance",view:"home"});`)
		require.Empty(t, newRun.Error)
	} else if origin != "regrant" {
		require.NoError(t, f.b.Handle(t.Context(), message(73102, 101, "/passes")))
	}
	var manual botdelivery.Intent
	if origin != "regrant" {
		manual = pendingPassReceipt(t, f)
		if origin != "home" {
			require.Nil(t, manual.Reference.Source)
		} else {
			require.NotNil(t, manual.Reference.Source)
		}
		require.Equal(t, target, manual.Target)
	}
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), original.QueueReference()))
	var operation, effect string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE reference->>'family'=$1 AND target_message_id=$2`, botdelivery.PassReceiptRedactionFamily, target).
		Scan(&operation, &effect))
	cleanup := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	drainOtherPassFixtureIntents(t, f, cleanup)
	queued, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, cleanup, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, queued.State)
	require.Zero(t, queued.Attempt)
	if origin == "regrant" {
		_, err = f.db.Exec(t.Context(), `INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice')`)
		require.NoError(t, err)
		card := passMenuCard(t, f, 101)
		var before, after string
		callbacks := `SELECT COALESCE(string_agg(to_jsonb(b)::text,'|' ORDER BY token),'') FROM bot.pass_buttons b WHERE owner='alice'`
		require.NoError(t, f.db.QueryRow(t.Context(), callbacks).Scan(&before))
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), cleanup))
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), cleanup))
		require.Equal(t, card, passMenuCard(t, f, 101))
		require.NoError(t, f.db.QueryRow(t.Context(), callbacks).Scan(&after))
		require.Equal(t, before, after)
		current, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, cleanup, false)
		require.NoError(t, readErr)
		require.Equal(t, delivery.Cancelled, current.State)
		require.Zero(t, current.Attempt)
		return
	}
	if origin == "concurrent_manual" {
		assertPassCleanupConcurrentFinish(t, f, manual, cleanup, target)
		return
	}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), cleanup))
	// FIFO admits the queued manual card first; cleanup cannot edit the old parent yet.
	beforeManual, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, cleanup, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, beforeManual.State)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), manual.QueueReference()))
	actualManual, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, manual.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, actualManual.State)
	newCard := passMenuCard(t, f, 101)
	require.Equal(t, target, newCard.ID)
	require.NotEmpty(t, newCard.Markup.Rows)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), cleanup))
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), cleanup))
	require.Equal(t, newCard, passMenuCard(t, f, 101))
	retired, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, cleanup, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Cancelled, retired.State)
	require.Zero(t, retired.Attempt)
	var callbacks int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons
 WHERE owner='alice' AND revision=$1`, manual.Reference.Revision).Scan(&callbacks))
	require.Positive(t, callbacks)
	var hash string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT view_hash FROM bot.pass_views WHERE owner='alice'`).Scan(&hash),
	)
	require.Equal(t, actualManual.Receipt.ViewHash, hash)
	require.NoError(t, f.b.Handle(t.Context(), aliceCallback(73103, target, newCard.Markup.Rows[0][0].Data)))
}

func assertPassCleanupConcurrentFinish(t *testing.T, f *fixture, manual botdelivery.Intent,
	cleanup delivery.Reference, target int64,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	wire, allowFinish, lane, allowLane := make(
		chan struct{},
	), make(
		chan struct{},
	), make(
		chan struct{},
	), make(
		chan struct{},
	)
	var finishOnce, laneOnce sync.Once
	releaseFinish := func() { finishOnce.Do(func() { close(allowFinish) }) }
	releaseLane := func() { laneOnce.Do(func() { close(allowLane) }) }
	defer releaseFinish()
	defer releaseLane()
	f.b.TG.HTTP = &http.Client{Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(request)
		if err == nil && strings.HasSuffix(request.URL.Path, "/editMessageText") {
			close(wire)
			select {
			case <-allowFinish:
			case <-ctx.Done():
				return response, ctx.Err()
			}
		}
		return response, err
	})}
	manualDone := make(chan error, 1)
	go func() { manualDone <- f.b.DeliverBotIntent(ctx, manual.QueueReference()) }()
	select {
	case <-wire:
	case <-ctx.Done():
		t.Fatal("manual transport did not reach completion barrier")
	}
	observed, err := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, cleanup, false)
	require.NoError(t, err)
	config := f.db.Config()
	var traceOnce sync.Once
	config.ConnConfig.Tracer = retirementCompletionTracer{before: func(query pgx.TraceQueryStartData) {
		if strings.Contains(query.SQL, "SELECT next_sequence FROM core.delivery_lanes") {
			traceOnce.Do(func() {
				close(lane)
				select {
				case <-allowLane:
				case <-ctx.Done():
				}
			})
		}
	}}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer func() {
		releaseLane()
		releaseFinish()
		cancel()
		pool.Close()
	}()
	service := botdelivery.Service{DB: pool, Delivery: f.b.Delivery}
	cleanupDone := make(chan error, 1)
	go func() {
		_, beginErr := service.Begin(ctx, botdelivery.BeginRequest{Observed: observed, Target: target})
		cleanupDone <- beginErr
	}()
	select {
	case <-lane:
	case <-ctx.Done():
		t.Fatal("cleanup did not reach lane barrier")
	}
	// Finish uses the real intent/lane transaction while cleanup has only its
	// earlier domain locks. Receipt continuation may wait until cleanup rolls back.
	releaseFinish()
	require.Eventually(t, func() bool {
		current, readErr := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, manual.QueueReference(), false)
		return readErr == nil && current.State == delivery.Succeeded
	}, 10*time.Second, 10*time.Millisecond)
	before := passAdmissionSnapshot(t, f)
	card := passMenuCard(t, f, 101)
	releaseLane()
	select {
	case err = <-cleanupDone:
		require.ErrorContains(t, err, "pass_receipt_changed")
	case <-ctx.Done():
		t.Fatal("cleanup did not release its transaction")
	}
	require.Equal(t, before, passAdmissionSnapshot(t, f), "lane, pacing, fairness and queue admission rolled back")
	select {
	case err = <-manualDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("manual continuation did not complete")
	}
	f.b.TG.HTTP = nil
	current, err := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, cleanup, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, current.State)
	require.Zero(t, current.Attempt)
	require.NoError(t, f.b.DeliverBotIntent(ctx, cleanup))
	require.NoError(t, f.b.DeliverBotIntent(ctx, cleanup))
	current, err = botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, cleanup, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Cancelled, current.State)
	require.Zero(t, current.Attempt)
	require.Equal(t, card, passMenuCard(t, f, 101))
	var callbacks int
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1`,
		manual.Reference.Revision).Scan(&callbacks))
	require.Positive(t, callbacks)
}

func passAdmissionSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var snapshot string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT COALESCE(string_agg(value,'|' ORDER BY value),'') FROM (
 SELECT 'lane:'||to_jsonb(l)::text AS value FROM core.delivery_lanes l WHERE bot_id=$1
 UNION ALL SELECT 'pacing:'||to_jsonb(p)::text FROM core.delivery_pacing p WHERE bot_id=$1
 UNION ALL SELECT 'fairness:'||to_jsonb(f)::text FROM core.delivery_fairness f WHERE bot_id=$1
 UNION ALL SELECT 'queue:'||to_jsonb(q)::text FROM core.delivery_queue q WHERE bot_id=$1
) snapshots`, f.b.Delivery.BotID).Scan(&snapshot))
	return snapshot
}

func pendingPassReceipt(t *testing.T, f *fixture) botdelivery.Intent {
	t.Helper()
	var operation, effect string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'family'='passes' AND state='pending'
 ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
	i, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}, false)
	require.NoError(t, err)
	return i
}

type passSuccessfulScenario struct {
	f               *fixture
	mode, authority string
	previous        int64
	priorCard       telegram.Message
	preservePrior   bool
	original        botdelivery.Intent
	source          string
}

type passSuccessfulTransport struct {
	revoked, refused               bool
	privateSends, latePrivateSends int
}

func TestDerivedPassReceiptRetiresActualSuccessfulSend(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"initial", "fallback", "fallback_manual", "fallback_new_source"} {
		for _, authority := range []string{"history", "current_grant"} {
			t.Run(
				mode+"_"+authority,
				func(t *testing.T) { t.Parallel(); runSuccessfulPassScenario(t, mode, authority) },
			)
		}
	}
}
func runSuccessfulPassScenario(t *testing.T, mode, authority string) {
	t.Helper()
	setup := setupSuccessfulPassScenario(t, mode, authority)
	f := setup.f
	state := installSuccessfulPassTransport(t, f, mode, authority)
	pumpBotDeliveries(t, f.b)
	pumpBotDeliveries(t, f.b)
	require.True(t, state.revoked, "revoke only after the provider persisted the private send")
	require.Equal(t, strings.HasPrefix(mode, "fallback"), state.refused)
	require.Equal(t, 1, state.privateSends)
	require.Zero(t, state.latePrivateSends, "no replacement private send after revocation")
	completed, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, setup.original.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, completed.State)
	require.True(t, completed.ContinuationDone)
	require.Positive(t, completed.MessageID)
	require.NotEqual(t, setup.previous, completed.MessageID)
	require.Equal(t, setup.original.Reference, completed.Reference)
	assertSuccessfulPassView(t, f, setup.source, setup.preservePrior)
	for _, target := range []int64{setup.previous, completed.MessageID} {
		assertSuccessfulPassTarget(t, setup, target)
	}
}
func installSuccessfulPassTransport(t *testing.T, f *fixture, mode, authority string) *passSuccessfulTransport {
	t.Helper()
	state := &passSuccessfulTransport{}
	f.b.TG.HTTP = &http.Client{Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
		if strings.HasPrefix(mode, "fallback") && !state.refused &&
			strings.HasSuffix(request.URL.Path, "/editMessageText") {
			state.refused = true
			return passRefusedEditResponse(request), nil
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		privateSend := strings.HasSuffix(request.URL.Path, "/sendMessage") && bytes.Contains(body, []byte("passmenu:"))
		if privateSend {
			state.privateSends++
			if state.revoked {
				state.latePrivateSends++
			}
		}
		response, requestErr := http.DefaultTransport.RoundTrip(request)
		if requestErr != nil || state.revoked || !privateSend {
			return response, requestErr
		}
		state.revoked = true
		if authority == "history" {
			deletePassDeliveryHistory(t, f, "alice")
		} else {
			_, sqlErr := f.db.Exec(
				t.Context(),
				`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
			)
			require.NoError(t, sqlErr)
		}
		return response, requestErr
	})}
	return state
}
func passRefusedEditResponse(request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{},
		Request:    request,
		Body: io.NopCloser(
			strings.NewReader(`{"ok":false,"error_code":400,"description":"message to edit not found"}`),
		),
	}
}
func assertSuccessfulPassTarget(t *testing.T, setup passSuccessfulScenario, target int64) {
	t.Helper()
	if target == 0 {
		return
	}
	f := setup.f
	if target == setup.previous && setup.preservePrior {
		require.Equal(t, setup.priorCard, passMenuCardOrEmpty(t, f, target))
		var effects int
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents WHERE reference->>'family'=$1 AND target_message_id=$2`, botdelivery.PassReceiptRedactionFamily, target).
				Scan(&effects),
		)
		require.Zero(t, effects)
		return
	}
	var effects int
	var terminal bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*),bool_and(state='sent' AND phase='edit') FROM bot.delivery_intents WHERE owner='alice' AND reference->>'family'=$1 AND target_message_id=$2`, botdelivery.PassReceiptRedactionFamily, target).
			Scan(&effects, &terminal),
	)
	require.Equal(t, 1, effects)
	require.True(t, terminal)
	card := passMenuCardOrEmpty(t, f, target)
	require.Equal(t, target, card.ID)
	require.Empty(t, card.Markup.Rows)
	require.Contains(t, card.Text, "/passes")
}
func setupSuccessfulPassScenario(t *testing.T, mode, authority string) passSuccessfulScenario {
	t.Helper()
	f := passMenuFixture(t)
	var err error
	if mode == "fallback_manual" {
		handlePassVisible(t, f, message(72599, 101, "/passes"))
	}
	run := runPassVM(t, f, 72600, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, run.Error)
	var previous int64
	if strings.HasPrefix(mode, "fallback") && mode != "fallback_manual" {
		pumpBotDeliveries(t, f.b)
		previous = passMenuCard(t, f, 101).ID
	}
	if mode == "fallback_manual" {
		previous = passMenuCard(t, f, 101).ID
	}
	if mode == "fallback_new_source" {
		newRun := runPassVM(
			t,
			f,
			72601,
			101,
			"Show my pass again",
			`return tools.passes.registration.show({event:"dance",view:"home"});`,
		)
		require.Empty(t, newRun.Error)
	}
	drainOtherPassFixtureIntents(t, f)
	priorCard := passMenuCardOrEmpty(t, f, previous)
	preservePrior := previous > 0 && (authority == "current_grant" || mode == "fallback_manual")
	if authority == "current_grant" {
		_, err = f.db.Exec(
			t.Context(),
			`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice');
 UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
		)
		require.NoError(t, err)
	}
	if mode == "fallback" {
		require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
	}
	original := pendingPassReceipt(t, f)
	var source string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT (state->'source')::text FROM bot.pass_views WHERE owner='alice'`).
			Scan(&source),
	)

	return passSuccessfulScenario{f, mode, authority, previous, priorCard, preservePrior, original, source}
}
func assertSuccessfulPassView(t *testing.T, f *fixture, source string, preservePrior bool) {
	t.Helper()
	var retiredSource string
	var redacted bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT (state->'source')::text,
 COALESCE((state->>'redacted')::boolean,false) FROM bot.pass_views WHERE owner='alice'`).Scan(&retiredSource, &redacted))
	require.Equal(t, !preservePrior, redacted)
	require.Equal(t, source, retiredSource)
	var callbacks int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice'`).
			Scan(&callbacks),
	)
	if preservePrior {
		require.Positive(t, callbacks)
	} else {
		require.Zero(t, callbacks)
	}
}
func TestDerivedPassReceiptViewRacePreservesAuthorizedCard(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	var err error
	run := runPassVM(t, f, 72700, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, run.Error)
	pumpBotDeliveries(t, f.b)
	require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
	original := pendingPassReceipt(t, f)
	var held pgx.Tx
	var holder int32
	ready := make(chan struct{})
	f.b.TG.HTTP = &http.Client{Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
		response, requestErr := http.DefaultTransport.RoundTrip(request)
		if err == nil && held == nil && strings.HasSuffix(request.URL.Path, "/editMessageText") {
			held, err = f.db.Begin(t.Context())
			if err != nil {
				return nil, err
			}
			if err = held.QueryRow(t.Context(), `SELECT pg_backend_pid() FROM core.users WHERE id='alice' FOR UPDATE`).
				Scan(&holder); err != nil {
				return nil, err
			}
			close(ready)
		}
		return response, requestErr
	})}
	finished := make(chan error, 1)
	go func() { finished <- f.b.DeliverBotIntent(t.Context(), original.QueueReference()) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the provider edit did not reach the receipt boundary")
	}
	defer func() { _ = held.Rollback(t.Context()) }()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := f.db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE '%core.users%')`, holder).Scan(&waiting)
		require.NoError(t, queryErr)
		return waiting
	}, 10*time.Second, 50*time.Millisecond, "receipt must read menu A before blocking on the owner lock")
	_, err = held.Exec(
		t.Context(),
		`UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"events"') WHERE owner='alice'`,
	)
	require.NoError(t, err)
	require.NoError(t, held.Commit(t.Context()))
	select {
	case err = <-finished:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the receipt did not complete after the view race")
	}
	completed, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, original.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, completed.State)
	require.True(t, completed.ContinuationDone)
	require.Equal(t, original.Reference, completed.Reference)
	var redacted bool
	var callbacks, cleanup int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((state->>'redacted')::boolean,false) FROM bot.pass_views WHERE owner='alice'`).
			Scan(&redacted),
	)
	require.False(t, redacted)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice'`).Scan(&callbacks),
	)
	require.Positive(t, callbacks)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents WHERE owner='alice'
 AND reference->>'family' IN ('pass_redaction',$1)`, botdelivery.PassReceiptRedactionFamily).Scan(&cleanup))
	require.Zero(t, cleanup)
	card := passMenuCard(t, f, 101)
	require.Equal(t, completed.MessageID, card.ID)
	require.NotEmpty(t, card.Markup.Rows)
}

func TestPassReceiptRetirementCannotBePubliclyEnqueued(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	var err error
	reference := botdelivery.Reference{
		Kind: botdelivery.CardIntent, Family: botdelivery.PassReceiptRedactionFamily, CardKey: "passes",
		Revision: 1, Version: 42, Object: "card:1", ResultKind: "view",
	}
	require.ErrorContains(t, f.b.Host.EnqueueBotCard(t.Context(), botdelivery.CardRequest{
		Owner: "alice", Chat: 101, Target: 42, Reference: reference,
	}), botdelivery.ErrBinding.Code)
	_, err = f.b.Host.EnqueueBotDelivery(t.Context(), botdelivery.EnqueueRequest{
		Owner: "alice", Chat: 101, Operation: "forged", Effect: "view", Phase: "send", Reference: reference,
	})
	require.ErrorContains(t, err, botdelivery.ErrBinding.Code)
}

func TestDerivedPassReceiptCombinedViewAndAuthorityRace(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"fallback_newer_denied", "fallback_newer_valid", "rendered_capability_denied", "rendered_capability_valid"} {
		t.Run(scenario, func(t *testing.T) { t.Parallel(); runCombinedPassReceiptRace(t, scenario) })
	}
}

func TestPassPreparationViewAndPayloadAuthorityRace(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"denied", "valid", "prior_home", "newer_denied", "newer_valid", "new_source_denied", "prior_manual"} {
		t.Run(scenario, func(t *testing.T) { t.Parallel(); runPassPreparationRace(t, scenario) })
	}
}

func TestPassReceiptMissingViewPreservesActualAuthority(t *testing.T) {
	t.Parallel()
	for _, revoke := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "valid", true: "revoked"}[revoke],
			func(t *testing.T) { t.Parallel(); runPassMissingViewRace(t, revoke) },
		)
	}
}

func passMenuCardOrEmpty(t *testing.T, f *fixture, target int64) telegram.Message {
	t.Helper()
	for _, card := range chatMessages(t, f, 101) {
		if card.ID == target {
			return card
		}
	}
	return telegram.Message{}
}

func TestPassReconstructionFailureUsesOriginalSuccessfulReceipt(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"denied", "valid", "prior_manual", "grant_denied"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			if scenario == "prior_manual" {
				handlePassVisible(t, f, message(73100, 101, "/passes"))
			} else {
				run := runPassVM(
					t,
					f,
					73100,
					101,
					"Show my pass",
					`return tools.passes.registration.show({event:"dance",view:"home"});`,
				)
				require.Empty(t, run.Error)
				_, err := f.db.Exec(
					t.Context(),
					`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice'); UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
				)
				require.NoError(t, err)
				pumpBotDeliveries(t, f.b)
			}
			originalCard := passMenuCard(t, f, 101)
			if scenario == "prior_manual" {
				run := runPassVM(
					t,
					f,
					73101,
					101,
					"Show my pass",
					`return tools.passes.registration.show({event:"dance",view:"home"});`,
				)
				require.Empty(t, run.Error)
			} else {
				require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
			}
			pending := pendingPassReceipt(t, f)
			require.Equal(t, originalCard.ID, pending.Target)
			public, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Independent public replacement"})
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE bot.pass_views SET revision=revision+1,state='{"view":"events"}',message_id=$1 WHERE owner='alice'`,
				public.ID,
			)
			require.NoError(t, err)
			if scenario == "grant_denied" {
				_, err = f.db.Exec(
					t.Context(),
					`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
				)
				require.NoError(t, err)
			} else if scenario != "valid" {
				deletePassDeliveryHistory(t, f, "alice")
			}
			before := len(chatMessages(t, f, 101))
			pumpBotDeliveries(t, f.b)
			pumpBotDeliveries(t, f.b)
			require.Len(t, chatMessages(t, f, 101), before, "retirement must only edit")
			current, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, pending.QueueReference(), false)
			require.NoError(t, err)
			require.Equal(t, delivery.Cancelled, current.State)
			require.Zero(t, current.Attempt)
			actual := passMenuCardOrEmpty(t, f, originalCard.ID)
			var cleanup int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents WHERE reference->>'family'=$1 AND target_message_id=$2`, botdelivery.PassReceiptRedactionFamily, originalCard.ID).
					Scan(&cleanup),
			)
			if scenario == "denied" || scenario == "grant_denied" {
				require.Empty(t, actual.Markup.Rows)
				require.Contains(t, actual.Text, "/passes")
				require.Equal(t, 1, cleanup)
			} else {
				require.Equal(t, originalCard, actual)
				require.Zero(t, cleanup)
			}
			require.Equal(t, "Independent public replacement", passMenuCardOrEmpty(t, f, public.ID).Text)
			var target int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT message_id FROM bot.pass_views WHERE owner='alice'`).Scan(&target),
			)
			require.Equal(t, public.ID, target)
		})
	}
}

func drainOtherPassFixtureIntents(t *testing.T, f *fixture, keep ...delivery.Reference) {
	t.Helper()
	rows, err := f.db.Query(
		t.Context(),
		`SELECT operation_key,effect_key FROM bot.delivery_intents WHERE owner='alice' AND state='pending' AND reference->>'family'<>'passes'`,
	)
	require.NoError(t, err)
	var refs []delivery.Reference
	for rows.Next() {
		var ref delivery.Reference
		ref.Owner = delivery.Bot
		require.NoError(t, rows.Scan(&ref.Key, &ref.Effect))
		refs = append(refs, ref)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	for _, ref := range refs {
		if slices.Contains(keep, ref) {
			continue
		}
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
	}
}

func countPassFixtureCards(t *testing.T, f *fixture) int {
	t.Helper()
	count := 0
	for _, card := range chatMessages(t, f, 101) {
		for _, row := range card.Markup.Rows {
			for _, button := range row {
				if strings.HasPrefix(button.Data, "passmenu:") {
					count++
					goto nextCard
				}
			}
		}
	nextCard:
	}
	return count
}

func runCombinedPassReceiptRace(t *testing.T, scenario string) {
	t.Helper()
	f := passMenuFixture(t)
	var err error
	run := runPassVM(t, f, 72800, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, run.Error)
	fallback := strings.HasPrefix(scenario, "fallback")
	denied := strings.HasSuffix(scenario, "denied")
	var previous int64
	if fallback {
		pumpBotDeliveries(t, f.b)
		previous = passMenuCard(t, f, 101).ID
		require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
	} else {
		_, err = f.db.Exec(
			t.Context(),
			`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice');
 UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
		)
		require.NoError(t, err)
	}
	original := pendingPassReceipt(t, f)
	var oldCallbacks int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1`, original.Reference.Revision).
			Scan(&oldCallbacks),
	)
	require.Positive(t, oldCallbacks)
	state := installCombinedPassTransport(t, f, fallback, denied, previous, original)
	pumpBotDeliveries(t, f.b)
	pumpBotDeliveries(t, f.b)
	require.True(
		t,
		state.changed,
		"combine view replacement with authority decision only after actual successful send",
	)
	require.Equal(t, fallback, state.refused)
	completed, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, original.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, completed.State)
	require.True(t, completed.ContinuationDone)
	require.Equal(t, original.Reference, completed.Reference)
	require.NotNil(t, completed.Receipt.Pass)
	require.Equal(t, previous, completed.Receipt.Pass.PreviousMessageID)
	if !fallback {
		require.Equal(t, "dance", completed.Receipt.Pass.Event)
		require.Equal(
			t,
			"proof_accept",
			completed.Receipt.Pass.Capability,
			"the events replacement cannot erase the sent payment queue authority",
		)
	}
	var callbacks, cleanup int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1`, original.Reference.Revision).
			Scan(&callbacks),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents WHERE owner='alice' AND reference->>'family'=$1
 AND target_message_id=ANY($2)`, botdelivery.PassReceiptRedactionFamily, []int64{previous, completed.MessageID}).
			Scan(&cleanup),
	)
	if denied {
		require.Zero(t, callbacks)
		expected := 1
		if fallback {
			expected = 2
		}
		require.Equal(t, expected, cleanup)
	} else {
		require.Positive(t, callbacks)
		require.Zero(t, cleanup)
	}
	replacement, replacementState := state.replacement, state.replacementState
	assertCombinedPassCards(t, f, completed, previous, replacement, denied)
	if fallback {
		var currentState string
		var revision, currentTarget int64
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT state::text,revision,message_id FROM bot.pass_views WHERE owner='alice'`).
				Scan(&currentState, &revision, &currentTarget),
		)
		require.Equal(t, replacementState, currentState)
		require.Equal(t, original.Reference.Revision+1, revision)
		require.Equal(t, replacement, currentTarget)
		var newCallbacks int
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1`, revision).
				Scan(&newCallbacks),
		)
		require.Equal(t, 1, newCallbacks)
	}
}

func runPassPreparationRace(t *testing.T, scenario string) {
	t.Helper()
	f := passMenuFixture(t)
	var err error
	if scenario == "prior_manual" {
		handlePassVisible(t, f, message(72900, 101, "/passes"))
	} else {
		run := runPassVM(t, f, 72900, 101, "Show my pass",
			`return tools.passes.registration.show({event:"dance",view:"home"});`)
		require.Empty(t, run.Error)
	}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice')`,
	)
	require.NoError(t, err)
	if scenario != "prior_home" && scenario != "prior_manual" {
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
		)
		require.NoError(t, err)
	}
	pumpBotDeliveries(t, f.b)
	priorCard := passMenuCard(t, f, 101)
	require.NotEmpty(t, priorCard.Markup.Rows)
	var priorRevision int64
	var priorCallbacks []string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT revision FROM bot.pass_views WHERE owner='alice'`).
			Scan(&priorRevision),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT array_agg(token ORDER BY token) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1`, priorRevision).
			Scan(&priorCallbacks),
	)
	require.NotEmpty(t, priorCallbacks)
	if scenario == "new_source_denied" || scenario == "prior_manual" {
		run := runPassVM(t, f, 72901, 101, "Show my pass again",
			`return tools.passes.registration.show({event:"dance",view:"home"});`)
		require.Empty(t, run.Error)
	}
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
	)
	require.NoError(t, err)
	if scenario != "new_source_denied" && scenario != "prior_manual" {
		require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
	}
	pending := pendingPassReceipt(t, f)
	require.Equal(t, priorCard.ID, pending.Target)
	drainOtherPassFixtureIntents(t, f)
	beforeMessages := len(chatMessages(t, f, 101))
	state := installPassPreparationTransport(t, f, scenario, pending)
	pumpBotDeliveries(t, f.b)
	pumpBotDeliveries(t, f.b)
	publicTarget := state.publicTarget
	require.True(t, state.changed, "change only after private reconstruction and before host admission")
	cancelled, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, pending.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Cancelled, cancelled.State)
	require.Zero(t, cancelled.Attempt, "no private wire was admitted")
	newMessages := 0
	if publicTarget > 0 {
		newMessages = 1
	}
	if scenario == "prior_manual" {
		require.Equal(t, 1, countPassFixtureCards(t, f))
	} else {
		require.Len(t, chatMessages(t, f, 101), beforeMessages+newMessages, "cleanup only edits existing targets")
	}
	retired := !strings.HasSuffix(scenario, "valid") && scenario != "prior_home" && scenario != "prior_manual"
	assertPassPreparationCards(t, f, priorCard, publicTarget, retired)
	var retainedCallbacks int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1 AND token=ANY($2)`, priorRevision, priorCallbacks).
			Scan(&retainedCallbacks),
	)
	if retired {
		require.Zero(t, retainedCallbacks)
	} else {
		require.Equal(t, len(priorCallbacks), retainedCallbacks, "authorized prior callbacks remain intact")
	}
	var edits int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'family'=$1 AND target_message_id=$2`,
		botdelivery.PassReceiptRedactionFamily, priorCard.ID).Scan(&edits))
	if retired {
		require.Equal(t, 1, edits)
	} else {
		require.Zero(t, edits)
	}
	if publicTarget > 0 {
		var current int64
		var callbacks int
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT message_id FROM bot.pass_views WHERE owner='alice'`).
				Scan(&current),
		)
		require.Equal(t, publicTarget, current)
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND token='preparation-public-callback'`).
				Scan(&callbacks),
		)
		require.Equal(t, 1, callbacks)
	}
}

func runPassMissingViewRace(t *testing.T, revoke bool) {
	t.Helper()
	f := passMenuFixture(t)
	var err error
	run := runPassVM(t, f, 73000, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, run.Error)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice');
 UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
	)
	require.NoError(t, err)
	pending := pendingPassReceipt(t, f)
	changed := false
	f.b.TG.HTTP = &http.Client{
		Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
			response, requestErr := http.DefaultTransport.RoundTrip(request)
			if requestErr == nil && !changed && strings.HasSuffix(request.URL.Path, "/sendMessage") {
				changed = true
				_, err = f.db.Exec(t.Context(), `DELETE FROM bot.pass_views WHERE owner='alice'`)
				require.NoError(t, err)
				if revoke {
					_, err = f.db.Exec(
						t.Context(),
						`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
					)
					require.NoError(t, err)
				}
			}
			return response, requestErr
		}),
	}
	pumpBotDeliveries(t, f.b)
	pumpBotDeliveries(t, f.b)
	require.True(t, changed)
	actual, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, pending.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, actual.State)
	require.True(t, actual.ContinuationDone)
	require.NotNil(t, actual.Receipt.Pass)
	require.Equal(t, "proof_accept", actual.Receipt.Pass.Capability)
	found := false
	for _, card := range chatMessages(t, f, 101) {
		if card.ID == actual.MessageID {
			found = true
			if revoke {
				require.Empty(t, card.Markup.Rows)
				require.Contains(t, card.Text, "/passes")
			} else {
				require.NotEmpty(t, card.Markup.Rows)
			}
		}
	}
	require.True(t, found)
}

type combinedPassTransport struct {
	changed, refused bool
	replacement      int64
	replacementState string
}

func installCombinedPassTransport(
	t *testing.T,
	f *fixture,
	fallback, denied bool,
	previous int64,
	original botdelivery.Intent,
) *combinedPassTransport {
	t.Helper()
	state := &combinedPassTransport{}
	f.b.TG.HTTP = &http.Client{
		Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
			if fallback && !state.refused && strings.HasSuffix(request.URL.Path, "/editMessageText") {
				state.refused = true
				admitted, readErr := botdelivery.Read(
					t.Context(),
					f.db,
					f.b.Delivery.BotID,
					original.QueueReference(),
					false,
				)
				require.NoError(t, readErr)
				require.NotNil(t, admitted.Receipt.Pass)
				require.Equal(t, previous, admitted.Receipt.Pass.PreviousMessageID)
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header:     http.Header{},
					Request:    request,
					Body: io.NopCloser(
						strings.NewReader(
							`{"ok":false,"error_code":400,"description":"message to edit not found"}`,
						),
					),
				}, nil
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			privateSend := strings.HasSuffix(request.URL.Path, "/sendMessage") &&
				bytes.Contains(body, []byte("passmenu:"))
			response, requestErr := http.DefaultTransport.RoundTrip(request)
			if requestErr != nil || !privateSend || state.changed {
				return response, requestErr
			}
			{
				state.changed = true
				if fallback {
					public, sendErr := f.b.TG.Send(
						t.Context(),
						telegram.Send{ChatID: 101, Text: "Independent public replacement"},
					)
					require.NoError(t, sendErr)
					state.replacement = public.ID
					_, err = f.db.Exec(
						t.Context(),
						`UPDATE bot.pass_views SET revision=revision+1,state='{"view":"events"}',message_id=$1 WHERE owner='alice'`,
						state.replacement,
					)
					require.NoError(t, err)
					_, err = f.db.Exec(
						t.Context(),
						`INSERT INTO bot.pass_buttons(owner,token,revision,action) VALUES('alice','newer-public-callback',$1,'{}')`,
						original.Reference.Revision+1,
					)
					require.NoError(t, err)
					if denied {
						deletePassDeliveryHistory(t, f, "alice")
					}
				} else {
					_, err = f.db.Exec(
						t.Context(),
						`UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"events"') WHERE owner='alice'`,
					)
					require.NoError(t, err)
					if denied {
						_, err = f.db.Exec(
							t.Context(),
							`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
						)
						require.NoError(t, err)
					}
				}
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT state::text FROM bot.pass_views WHERE owner='alice'`).
						Scan(&state.replacementState),
				)
			}
			return response, requestErr
		}),
	}

	return state
}

func assertCombinedPassCards(
	t *testing.T,
	f *fixture,
	completed botdelivery.Intent,
	previous, replacement int64,
	denied bool,
) {
	t.Helper()
	found := map[int64]bool{}
	for _, card := range chatMessages(t, f, 101) {
		found[card.ID] = true
		if card.ID == completed.MessageID || (previous > 0 && card.ID == previous) {
			if denied {
				require.Empty(t, card.Markup.Rows)
				require.Contains(t, card.Text, "/passes")
			} else {
				require.NotEmpty(t, card.Markup.Rows)
			}
		}
		if card.ID == replacement {
			require.Equal(t, "Independent public replacement", card.Text)
		}
	}
	require.True(t, found[completed.MessageID])
	if previous > 0 {
		require.True(t, found[previous])
	}
	if replacement > 0 {
		require.True(t, found[replacement])
	}
}

type preparationPassTransport struct {
	changed      bool
	generations  int
	publicTarget int64
}

func installPassPreparationTransport(
	t *testing.T,
	f *fixture,
	scenario string,
	pending botdelivery.Intent,
) *preparationPassTransport {
	t.Helper()
	state := &preparationPassTransport{}
	var err error
	f.b.API.HTTP = &http.Client{
		Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
			response, requestErr := http.DefaultTransport.RoundTrip(request)
			if request.URL.Path == "/v1/me/history/generation" {
				state.generations++
			}
			if requestErr == nil && !state.changed && state.generations == 2 &&
				request.URL.Path == "/v1/me/history/generation" {
				state.changed = true
				if strings.HasPrefix(scenario, "newer") {
					public, sendErr := f.b.TG.Send(
						t.Context(),
						telegram.Send{ChatID: 101, Text: "Independent public replacement"},
					)
					require.NoError(t, sendErr)
					state.publicTarget = public.ID
					_, err = f.db.Exec(
						t.Context(),
						`UPDATE bot.pass_views SET revision=revision+1,state='{"view":"events"}',message_id=$1 WHERE owner='alice'`,
						state.publicTarget,
					)
					require.NoError(t, err)
					_, err = f.db.Exec(t.Context(), `INSERT INTO bot.pass_buttons(owner,token,revision,action)
 VALUES('alice','preparation-public-callback',$1,'{}')`, pending.Reference.Revision+1)
					require.NoError(t, err)
				} else {
					_, err = f.db.Exec(
						t.Context(),
						`UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"events"') WHERE owner='alice'`,
					)
					require.NoError(t, err)
				}
				if !strings.HasSuffix(scenario, "valid") {
					_, err = f.db.Exec(
						t.Context(),
						`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
					)
					require.NoError(t, err)
				}
			}
			return response, requestErr
		}),
	}

	return state
}

func assertPassPreparationCards(
	t *testing.T,
	f *fixture,
	priorCard telegram.Message,
	publicTarget int64,
	retired bool,
) {
	t.Helper()
	found := false
	for _, card := range chatMessages(t, f, 101) {
		if card.ID == priorCard.ID {
			found = true
			if retired {
				require.Empty(t, card.Markup.Rows)
				require.Contains(t, card.Text, "/passes")
			} else {
				require.Equal(t, priorCard, card, "new payload denial cannot retire an authorized prior card")
			}
		}
		if card.ID == publicTarget {
			require.Equal(t, "Independent public replacement", card.Text)
		}
	}
	require.True(t, found)
}
