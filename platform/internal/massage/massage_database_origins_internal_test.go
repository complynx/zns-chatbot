package massage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

type r32Row struct{ err error }

func (r r32Row) Scan(...any) error { return r.err }

type r32Tx struct {
	pgx.Tx

	err error
}

func (tx r32Tx) QueryRow(context.Context, string, ...any) pgx.Row { return r32Row{err: tx.err} }

func TestMassageR32SQLAndDomainControls(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		tx := r32Tx{err: failure}
		var pending []delivery.Registration
		err := queueNotice(t.Context(), tx, 1, &pending, "booking", "alice", "booked")
		require.ErrorIs(t, err, core.DatabaseOperationError(failure))
		require.Empty(t, pending)
		_, err = loadParty(t.Context(), tx, "event", "party", false)
		require.ErrorIs(t, err, core.DatabaseOperationError(failure))
		_, err = readLegacyDraft(t.Context(), tx, "alice", "event", "draft", false)
		require.ErrorIs(t, err, core.DatabaseOperationError(failure))
	}
	tx := r32Tx{err: pgx.ErrNoRows}
	var pending []delivery.Registration
	require.NoError(t, queueNotice(t.Context(), tx, 1, &pending, "booking", "alice", "booked"))
	require.Empty(t, pending)
	_, err := loadParty(t.Context(), tx, "event", "party", false)
	var domainError *core.ProblemError
	require.ErrorAs(t, err, &domainError)
	require.Equal(t, "not_found", domainError.Code)
	_, err = readLegacyDraft(t.Context(), tx, "alice", "event", "draft", false)
	require.ErrorAs(t, err, &domainError)
	require.Equal(t, "not_found", domainError.Code)
}

func r32Database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_massage_r32_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		require.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	_, err = db.Exec(t.Context(), `INSERT INTO core.massage_events(id) VALUES('r32');
 INSERT INTO core.massage_specialists(event_id,owner,name) VALUES('r32','alice','Provider');
 INSERT INTO core.legacy_massage_import_references(source_key,bot_id,source_kind,source_record_sha256,source_record,target_id,owner,event_id)
 VALUES(repeat('a',64),1,'draft',repeat('b',64),'{}','draft','alice','r32');
 INSERT INTO core.legacy_massage_drafts(id,source_key,owner,event_id,state)
 VALUES('draft',repeat('a',64),'alice','r32','{}')`)
	require.NoError(t, err)
	return db
}

func TestMassageR32JSONOrigins(t *testing.T) {
	t.Parallel()
	db := r32Database(t)
	for _, raw := range []string{`{"en":1}`, `null`, `{}`, `{"en":"description"}`} {
		_, err := db.Exec(t.Context(), `UPDATE core.massage_specialists SET about=$1::jsonb`, raw)
		require.NoError(t, err)
		values, err := readProviders(t.Context(), db, "r32", false)
		_, detailErr := (Service{DB: db}).ProviderDetail(t.Context(), "alice", "r32", "alice", "")
		if raw == `{"en":1}` {
			var decode *json.UnmarshalTypeError
			require.ErrorAs(t, err, &decode)
			require.False(t, core.IsDatabaseFailure(err))
			require.ErrorAs(t, detailErr, &decode)
			require.False(t, core.IsDatabaseFailure(detailErr))
			continue
		}
		require.NoError(t, err)
		require.NoError(t, detailErr)
		require.Len(t, values, 1)
		var expected map[string]string
		require.NoError(t, json.Unmarshal([]byte(raw), &expected))
		require.Equal(t, expected, values[0].About)
	}
	for _, raw := range []string{`{"length":"private-invalid"}`, `{}`, `{"length":2}`} {
		_, err := db.Exec(t.Context(), `UPDATE core.legacy_massage_drafts SET state=$1::jsonb`, raw)
		require.NoError(t, err)
		draft, err := readLegacyDraft(t.Context(), db, "alice", "r32", "draft", false)
		if strings.Contains(raw, "private-invalid") {
			var decode *json.UnmarshalTypeError
			require.ErrorAs(t, err, &decode)
			require.False(t, core.IsDatabaseFailure(err))
			continue
		}
		require.NoError(t, err)
		var expected LegacyState
		require.NoError(t, json.Unmarshal([]byte(raw), &expected))
		require.Equal(t, expected, draft.State)
	}
	r32ReceiptJSON(t, db)
}

func r32ReceiptJSON(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	command := Command{Key: "receipt", Action: "book", Event: "r32"}
	encoded, err := json.Marshal(command)
	require.NoError(t, err)
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	for _, raw := range []string{`{"length":"private-invalid"}`, `null`, `{}`, `{"id":"booking","length":2}`} {
		_, err = db.Exec(
			t.Context(),
			`INSERT INTO core.massage_operations(actor,key,request_hash,result) VALUES('alice','receipt',$1,$2::jsonb)
  ON CONFLICT(actor,key) DO UPDATE SET result=excluded.result`,
			fingerprint,
			raw,
		)
		require.NoError(t, err)
		tx, beginErr := db.Begin(t.Context())
		require.NoError(t, beginErr)
		prepared, prepareErr := (Service{DB: db}).PrepareInTx(t.Context(), tx, "alice", command)
		require.NoError(t, tx.Rollback(t.Context()))
		if strings.Contains(raw, "private-invalid") {
			var decode *json.UnmarshalTypeError
			require.ErrorAs(t, prepareErr, &decode)
			require.False(t, core.IsDatabaseFailure(prepareErr))
			continue
		}
		require.NoError(t, prepareErr)
		require.True(t, prepared.found)
		var expected Reservation
		require.NoError(t, json.Unmarshal([]byte(raw), &expected))
		require.Equal(t, expected, prepared.result)
	}
	command.Length = 3
	_, err = (Service{DB: db}).Execute(t.Context(), "alice", command)
	var conflict *core.ProblemError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, "idempotency_conflict", conflict.Code)
}

type r32SQLFault struct {
	prefix   string
	fired    bool
	closeErr error
}

func (f *r32SQLFault) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if !f.fired && strings.HasPrefix(data.SQL, f.prefix) {
		f.fired = true
		f.closeErr = conn.Close(ctx)
	}
	return ctx
}
func (*r32SQLFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestMassageR32PreferenceSQLRollbackAndRecovery(t *testing.T) {
	t.Parallel()
	db := r32Database(t)
	fault := &r32SQLFault{prefix: "UPDATE core.massage_specialists"}
	config := db.Config()
	config.ConnConfig.Tracer = fault
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	_, err = (Service{DB: broken}).SetPreferences(t.Context(), "alice", "r32", Preferences{})
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
	require.True(t, fault.fired)
	require.NoError(t, fault.closeErr)
	current, err := (Service{DB: db}).Preferences(t.Context(), "alice", "r32")
	require.NoError(t, err)
	require.Equal(t, Preferences{Bookings: true, Next: true}, current)
	_, err = (Service{DB: db}).SetPreferences(t.Context(), "alice", "r32", Preferences{})
	require.NoError(t, err)
	current, err = (Service{DB: db}).Preferences(t.Context(), "alice", "r32")
	require.NoError(t, err)
	require.Equal(t, Preferences{}, current)
	_, err = (Service{DB: db}).SetPreferences(t.Context(), "bob", "r32", Preferences{})
	var forbidden *core.ProblemError
	require.ErrorAs(t, err, &forbidden)
	require.Equal(t, "forbidden", forbidden.Code)
}
