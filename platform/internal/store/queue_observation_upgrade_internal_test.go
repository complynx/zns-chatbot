package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPredecessorPassReceiptAuthorityAcross090(t *testing.T) {
	t.Parallel()
	for _, upgraded := range []bool{false, true} {
		for _, denied := range []bool{false, true} {
			t.Run(fmt.Sprintf("upgraded_%t_denied_%t", upgraded, denied), func(t *testing.T) {
				t.Parallel()
				runPredecessorPassReceiptAuthority(t, upgraded, denied, "")
			})
		}
	}
}

func TestPredecessorPassReceiptRejectsMalformedAndForeignPayload(t *testing.T) {
	t.Parallel()
	for _, corruption := range []string{"bad_hash", "foreign_card", "lost_pass"} {
		t.Run(corruption, func(t *testing.T) {
			t.Parallel()
			runPredecessorPassReceiptAuthority(t, true, false, corruption)
		})
	}
}

func runPredecessorPassReceiptAuthority(t *testing.T, upgraded, denied bool, corruption string) {
	t.Helper()
	db := creditObservationUpgradeDatabase(t)
	applyCreditObservationPredecessor(t, db)
	generation := int64(0)
	source := &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	// These are exactly the four fields written by the predecessor pass producer.
	// Fresh canonical rendering could change its hash and callback tokens.
	old := botdelivery.Continuation{Kind: "pass_card", Revision: 1,
		ViewHash: strings.Repeat("a", 64), Tokens: []string{strings.Repeat("b", 32)}}
	ref := botdelivery.Reference{Kind: botdelivery.CardIntent, Family: "passes", CardKey: "passes",
		Revision: 1, Generation: &generation, Source: source, Continuation: old}
	if corruption == "foreign_card" {
		ref.CardKey = "orders"
	}
	if corruption == "lost_pass" {
		ref.Continuation.Pass = &botdelivery.PassCardReceipt{}
	}
	refRaw, err := json.Marshal(ref)
	require.NoError(t, err)
	old.ViewHash = strings.Repeat("c", 64)
	if corruption == "bad_hash" {
		old.ViewHash = "invalid"
	}
	old.Tokens = []string{strings.Repeat("d", 32)}
	receiptRaw, err := json.Marshal(old)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.users(id,telegram_id,name) VALUES('old-pass-owner',101,'Synthetic')`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO bot.delivery_intents(bot_id,operation_key,effect_key,owner,chat_id,reference,state,phase,
 message_id,target_message_id,attempt,receipt,continuation_done)
 VALUES(4242,'old-pass','view','old-pass-owner',101,$1,'sent','edit',42,42,1,$2,true)`,
		refRaw,
		receiptRaw,
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO bot.pass_buttons(owner,token,revision,action)
 VALUES('old-pass-owner',$1,1,'{}')`, old.Tokens[0])
	require.NoError(t, err)
	var savedReference, savedReceipt string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT reference::text,receipt::text FROM bot.delivery_intents
 WHERE operation_key='old-pass'`).Scan(&savedReference, &savedReceipt))
	ledger := queueUpgradeLedgerSnapshot(t, db, "088_delivery_queue_observation.sql")
	if upgraded {
		require.NoError(t, Migrate(t.Context(), db))
		checkPassDeliveryTargetsUpgrade(t, db)
		require.Equal(t, ledger, queueUpgradeLedgerSnapshot(t, db, "088_delivery_queue_observation.sql"))
	}
	var actualReference, actualReceipt string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT reference::text,receipt::text FROM bot.delivery_intents
 WHERE operation_key='old-pass'`).Scan(&actualReference, &actualReceipt))
	require.Equal(t, savedReference, actualReference)
	require.Equal(t, savedReceipt, actualReceipt)
	s := botdelivery.Service{DB: db, Delivery: delivery.Settings{BotID: 4242,
		BotInterval: time.Millisecond, ChatInterval: time.Millisecond, Fallback: time.Second}}
	require.NoError(t, s.StorePassMenu(t.Context(), botdelivery.PassMenuRequest{Owner: "old-pass-owner",
		Chat: 101, Revision: 2, State: interaction.RegistrationMenu{View: "events"}, Source: source}))
	_, err = db.Exec(t.Context(), `UPDATE bot.pass_views SET message_id=42 WHERE owner='old-pass-owner'`)
	require.NoError(t, err)
	fresh := ref
	fresh.CardKey = "passes"
	fresh.Revision = 2
	fresh.Continuation = botdelivery.Continuation{Kind: "pass_card", Revision: 2,
		ViewHash: strings.Repeat("e", 64), Pass: &botdelivery.PassCardReceipt{PreviousMessageID: 42}}
	require.NoError(t, s.EnqueueCard(t.Context(), botdelivery.CardRequest{Owner: "old-pass-owner", Chat: 101,
		Target: 42, Reference: fresh}))
	var operation string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT operation_key FROM bot.delivery_intents
 WHERE state='pending' AND reference->>'family'='passes'`).Scan(&operation))
	pending, err := botdelivery.Read(t.Context(), db, 4242,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: "view"}, false)
	require.NoError(t, err)
	if denied {
		_, err = db.Exec(
			t.Context(),
			`INSERT INTO core.conversation_history_generations(owner,generation) VALUES('old-pass-owner',1)`,
		)
		require.NoError(t, err)
	}
	result, err := s.Begin(t.Context(), botdelivery.BeginRequest{Observed: pending, Target: 42,
		Pass: &botdelivery.PassCardReceipt{PreviousMessageID: 42}})
	if corruption != "" {
		require.ErrorIs(t, err, botdelivery.ErrBinding)
		unchanged, readErr := botdelivery.Read(t.Context(), db, 4242, pending.QueueReference(), false)
		require.NoError(t, readErr)
		require.Equal(t, delivery.Deferred, unchanged.State)
		require.Zero(t, unchanged.Attempt)
		return
	}
	require.NoError(t, err)
	require.Equal(t, !denied, result.Ready)
	if !denied {
		require.Equal(t, delivery.Sending, result.Intent.State)
		require.Equal(t, int64(42), result.Intent.Target)
		require.NotNil(t, result.Intent.Receipt.Pass)
	} else {
		require.Equal(t, delivery.Cancelled, result.Intent.State)
		require.Zero(t, result.Intent.Attempt)
		var cleanupOperation string
		require.NoError(t, db.QueryRow(t.Context(), `SELECT operation_key FROM bot.delivery_intents
 WHERE reference->>'family'=$1`, botdelivery.PassReceiptRedactionFamily).Scan(&cleanupOperation))
		cleanup, readErr := botdelivery.Read(t.Context(), db, 4242,
			delivery.Reference{Owner: delivery.Bot, Key: cleanupOperation, Effect: "view"}, false)
		require.NoError(t, readErr)
		admitted, beginErr := s.Begin(t.Context(), botdelivery.BeginRequest{Observed: cleanup, Target: 42})
		require.NoError(t, beginErr)
		require.True(t, admitted.Ready)
		require.Equal(t, "edit", admitted.Intent.Phase)
		require.Equal(t, int64(42), admitted.Intent.Target)
		var callbacks int
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='old-pass-owner'`).
				Scan(&callbacks),
		)
		require.Zero(t, callbacks)
	}
	replay, err := s.Begin(t.Context(), botdelivery.BeginRequest{Observed: pending, Target: 42,
		Pass: &botdelivery.PassCardReceipt{PreviousMessageID: 42}})
	require.NoError(t, err)
	require.False(t, replay.Ready)
	previous, err := botdelivery.Read(t.Context(), db, 4242,
		delivery.Reference{Owner: delivery.Bot, Key: "old-pass", Effect: "view"}, false)
	require.NoError(t, err)
	require.NoError(t, s.ApplyReceipt(t.Context(), botdelivery.ReceiptRequest{Observed: previous}))
	require.Nil(t, previous.Receipt.Pass)
	require.Equal(t, old, previous.Receipt)
}

func TestDeliveryQueueObservationUpgradeFrom087(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("TEST_DATABASE_URL required for isolated upgrade proof")
	}
	admin, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	suffix := make([]byte, 8)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "synthetic_qa_queue_upgrade_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)")
		require.NoError(t, dropErr)
	})
	// Construct the real pre-088 schema from actual embedded forward migrations,
	// recording the same immutable checksums used by the product migrator.
	_, err = db.Exec(t.Context(), `CREATE TABLE public.zns_schema_migrations
 (name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`)
	require.NoError(t, err)
	entries, err := migrations.ReadDir("migrations")
	require.NoError(t, err)
	const lastOld = "087_telegram_inbox_retries.sql"
	const upgrade = "088_delivery_queue_observation.sql"
	const credit = "089_credit_usage_observation.sql"
	const current = passDeliveryTargetsUpgrade
	foundOld, foundUpgrade := false, false
	expectedOldLedger := make([]queueUpgradeLedgerEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() > current {
			t.Fatalf(
				"upgrade proof is pinned to the 090 embedded schema epoch; newer migration %s requires a new scoped fixture",
				entry.Name(),
			)
		}
		if entry.Name() == upgrade {
			foundUpgrade = true
		}
		if entry.Name() > lastOld {
			continue
		}
		body, readErr := migrations.ReadFile("migrations/" + entry.Name())
		require.NoError(t, readErr)
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		expectedOldLedger = append(expectedOldLedger, queueUpgradeLedgerEntry{Name: entry.Name(), Checksum: checksum})
		tx, beginErr := db.Begin(t.Context())
		require.NoError(t, beginErr)
		_, applyErr := tx.Exec(t.Context(), string(body))
		if applyErr != nil {
			_ = tx.Rollback(t.Context())
			require.NoError(t, applyErr, entry.Name())
		}
		_, recordErr := tx.Exec(
			t.Context(),
			`INSERT INTO public.zns_schema_migrations(name,checksum) VALUES($1,$2)`,
			entry.Name(),
			checksum,
		)
		if recordErr != nil {
			_ = tx.Rollback(t.Context())
			require.NoError(t, recordErr)
		}
		require.NoError(t, tx.Commit(t.Context()))
		if entry.Name() == lastOld {
			foundOld = true
		}
	}
	require.True(t, foundOld)
	require.True(t, foundUpgrade)
	require.Equal(
		t,
		expectedOldLedger,
		queueUpgradeLedger(t, db),
		"exact through-087 ledger names and actual embedded byte checksums",
	)
	oldLedgerSnapshot := queueUpgradeLedgerSnapshot(t, db, lastOld)
	upgradeBody, err := migrations.ReadFile("migrations/" + upgrade)
	require.NoError(t, err)
	expectedUpgradedLedger := append(
		append([]queueUpgradeLedgerEntry(nil), expectedOldLedger...),
		queueUpgradeLedgerEntry{Name: upgrade, Checksum: fmt.Sprintf("%x", sha256.Sum256(upgradeBody))},
	)
	currentBody, err := migrations.ReadFile("migrations/" + current)
	require.NoError(t, err)
	creditBody, err := migrations.ReadFile("migrations/" + credit)
	require.NoError(t, err)
	expectedUpgradedLedger = append(expectedUpgradedLedger,
		queueUpgradeLedgerEntry{Name: credit, Checksum: fmt.Sprintf("%x", sha256.Sum256(creditBody))})
	expectedUpgradedLedger = append(expectedUpgradedLedger,
		queueUpgradeLedgerEntry{Name: current, Checksum: fmt.Sprintf("%x", sha256.Sum256(currentBody))})
	var hadTimestamp bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='core' AND table_name='delivery_queue' AND column_name='enqueued_at')`).
			Scan(&hadTimestamp),
	)
	require.False(t, hadTimestamp, "old schema must be genuine, not a modified current-schema fixture")
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.delivery_lanes(bot_id,chat,next_sequence,last_served) VALUES(4242,'101',3,11);
 INSERT INTO core.delivery_fairness(bot_id,grants) VALUES(4242,19);
 INSERT INTO core.delivery_pacing(bot_id,chat,not_before,pause_reason) VALUES(4242,'','infinity','telegram_invalid_cooldown');
 INSERT INTO core.delivery_queue(bot_id,owner_kind,owner_key,effect_key,chat,thread_id,lane_sequence,traffic_class,state,not_before)
 VALUES(4242,'orders','pending','notice','101',7,1,'interactive','pending','infinity'),
 (4242,'orders','sending','notice','101',7,2,'interactive','sending',statement_timestamp()+interval '1 hour'),
 (4242,'orders','uncertain','notice','101',7,3,'background','unknown','-infinity');
 INSERT INTO core.users(id,telegram_id,name) VALUES('upgrade-actor',987654,'Synthetic');
 INSERT INTO core.admin_messages(actor,key,request,state) VALUES('upgrade-actor','old-job','{}','queued');
 INSERT INTO core.admin_message_deliveries(message_id,destination,state,attempt,failure_count,bot_id,available_at,lease_until,failure)
 SELECT id,'{"chat":"101"}','pending',7,4,4242,'infinity',statement_timestamp()+interval '1 hour','telegram_rate_limit' FROM core.admin_messages`,
	)
	require.NoError(t, err)
	seedPassDeliveryTargetsUpgrade(t, db)
	// Compare the exact pre-upgrade durable scheduling state, not invented values.
	before := queueUpgradeState(t, db, false)
	require.NoError(t, Migrate(t.Context(), db))
	require.Equal(
		t,
		expectedUpgradedLedger,
		queueUpgradeLedger(t, db),
		"only actual 088, 089 and 090 ledger entries are added; old entries remain exact",
	)
	require.JSONEq(
		t,
		oldLedgerSnapshot,
		queueUpgradeLedgerSnapshot(t, db, lastOld),
		"the upgrade must preserve every old ledger field including applied timestamps",
	)
	upgradedLedgerSnapshot := queueUpgradeLedgerSnapshot(t, db, current)
	after := queueUpgradeState(t, db, true)
	require.JSONEq(t, before, after, "all existing order/retry/cooldown metadata must survive")
	checkPassDeliveryTargetsUpgrade(t, db)
	var unknown int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.delivery_queue WHERE enqueued_at IS NULL`).Scan(&unknown),
	)
	require.Equal(t, int64(3), unknown)
	items, err := delivery.QueueObservations(t.Context(), db, 4242)
	require.NoError(t, err)
	require.Len(t, items, 3)
	for _, item := range items {
		require.Equal(t, int64(1), item.UnknownAge)
		require.Zero(t, item.OldestAgeSeconds)
		require.Equal(t, int64(1), item.UnboundedDeadline)
	}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	ref := delivery.Reference{Owner: delivery.Food, Key: "new-row", Effect: "notice"}
	_, err = delivery.Register(t.Context(), tx, 4242, ref, delivery.Destination{Chat: "101"}, delivery.Background)
	if err != nil {
		_ = tx.Rollback(t.Context())
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(t, err)
	var original time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT enqueued_at FROM core.delivery_queue WHERE owner_key='new-row'`).
			Scan(&original),
	)
	require.False(t, original.IsZero())
	tx, err = db.Begin(t.Context())
	require.NoError(t, err)
	_, err = delivery.Register(t.Context(), tx, 4242, ref, delivery.Destination{Chat: "101"}, delivery.Background)
	if err != nil {
		_ = tx.Rollback(t.Context())
		require.NoError(t, err)
	}
	err = delivery.Project(t.Context(), tx, 4242, ref, delivery.Deferred, time.Now().Add(time.Hour))
	if err != nil {
		_ = tx.Rollback(t.Context())
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(t.Context()))
	var retained time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT enqueued_at FROM core.delivery_queue WHERE owner_key='new-row'`).
			Scan(&retained),
	)
	require.Equal(t, original, retained, "retry projection retains the initial enqueue age")
	replayState := queueUpgradeState(t, db, true)
	require.NoError(t, Migrate(t.Context(), db), "restart migration is idempotent")
	require.JSONEq(t, replayState, queueUpgradeState(t, db, true),
		"replay preserves every durable scheduling and receipt field")
	require.Equal(
		t,
		expectedUpgradedLedger,
		queueUpgradeLedger(t, db),
		"replay preserves exact migration versions and checksums",
	)
	require.JSONEq(
		t,
		upgradedLedgerSnapshot,
		queueUpgradeLedgerSnapshot(t, db, current),
		"replay preserves the complete ledger including applied timestamps",
	)
	checkPassDeliveryTargetsUpgrade(t, db)
	checkPassDeliveryTargetsPlan(t, db)
}

type queueUpgradeLedgerEntry struct {
	Name     string
	Checksum string
}

func queueUpgradeLedger(t *testing.T, db *pgxpool.Pool) []queueUpgradeLedgerEntry {
	t.Helper()
	rows, err := db.Query(t.Context(), `SELECT name,checksum FROM public.zns_schema_migrations ORDER BY name`)
	require.NoError(t, err)
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[queueUpgradeLedgerEntry])
	require.NoError(t, err)
	return entries
}

func queueUpgradeLedgerSnapshot(t *testing.T, db *pgxpool.Pool, through string) string {
	t.Helper()
	var snapshot string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(m) ORDER BY m.name)::text FROM public.zns_schema_migrations m WHERE m.name<=$1`, through).
			Scan(&snapshot),
	)
	return snapshot
}

func queueUpgradeState(t *testing.T, db *pgxpool.Pool, after bool) string {
	t.Helper()
	queueExpression := "to_jsonb(q)"
	if after {
		queueExpression = "to_jsonb(q)-'enqueued_at'"
	}
	query := `SELECT jsonb_build_object(
 'queue',(SELECT jsonb_agg(` + queueExpression + ` ORDER BY q.lane_sequence) FROM core.delivery_queue q),
 'lanes',(SELECT jsonb_agg(to_jsonb(l) ORDER BY l.chat) FROM core.delivery_lanes l),
 'pacing',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.chat) FROM core.delivery_pacing p),
 'fairness',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.bot_id) FROM core.delivery_fairness f),
 'jobs',(SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM core.admin_messages m),
 'attempts',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM core.admin_message_deliveries d),
 'bot_intents',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.bot_id,i.operation_key,i.effect_key) FROM bot.delivery_intents i))::text`
	var value string
	require.NoError(t, db.QueryRow(t.Context(), query).Scan(&value))
	return value
}

const passDeliveryTargetsUpgrade = "090_pass_delivery_targets.sql"

func seedPassDeliveryTargetsUpgrade(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	var present bool
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT to_regclass('bot.bot_pass_delivery_targets') IS NOT NULL`).Scan(&present))
	require.False(t, present, "the genuine predecessor schema does not contain the 090 index")
	_, err := db.Exec(t.Context(), `INSERT INTO bot.delivery_intents
 (bot_id,operation_key,effect_key,owner,chat_id,reference,state,phase,message_id,target_message_id,attempt,receipt)
 VALUES(4242,'old-pass','view','upgrade-actor',101,'{"family":"passes"}','sent','send',42,0,1,'{"tokens":["retained"]}'),
 (4242,'pending-pass','view','upgrade-actor',101,'{"family":"passes"}','pending','edit',0,42,0,'{}'),
 (4242,'other-family','view','upgrade-actor',101,'{"family":"orders"}','sent','send',43,0,1,'{}')`)
	require.NoError(t, err)
}

func checkPassDeliveryTargetsUpgrade(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	var definition string
	require.NoError(t, db.QueryRow(
		t.Context(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname='bot' AND indexname='bot_pass_delivery_targets'`,
	).Scan(&definition))
	require.Equal(
		t,
		"CREATE INDEX bot_pass_delivery_targets ON bot.delivery_intents USING btree (bot_id, owner, chat_id, message_id, attempted_at DESC NULLS LAST, created_at DESC, operation_key DESC, effect_key DESC) WHERE ((state = 'sent'::text) AND ((reference ->> 'family'::text) = 'passes'::text))",
		definition,
		"090 has the exact bounded receipt lookup keys, deterministic order and private pass predicate",
	)
}

func checkPassDeliveryTargetsPlan(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(t.Context(), `INSERT INTO bot.delivery_intents
 (bot_id,operation_key,effect_key,owner,chat_id,reference,state,phase,message_id)
 SELECT 4242,'plan-history-'||n,'view','unrelated-owner',202,'{"family":"passes"}','sent','send',n
 FROM generate_series(1,10000) n`)
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), "ANALYZE bot.delivery_intents")
	require.NoError(t, err)
	var plan string
	require.NoError(t, tx.QueryRow(t.Context(), `EXPLAIN (FORMAT JSON)
 SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE bot_id=$1 AND owner=$2 AND chat_id=$3 AND state='sent' AND message_id=$4
 AND reference->>'family'='passes'
 ORDER BY attempted_at DESC NULLS LAST,created_at DESC,operation_key DESC,effect_key DESC LIMIT 1`,
		int64(4242), "upgrade-actor", int64(101), int64(42)).Scan(&plan))
	require.Contains(t, plan, `"Index Name": "bot_pass_delivery_targets"`,
		"the bounded successful-receipt lookup uses 090 with large unrelated history")
	require.Contains(t, plan, `"Node Type": "Limit"`)
}
