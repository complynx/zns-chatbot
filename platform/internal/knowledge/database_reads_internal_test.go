package knowledge

import (
	"context"
	"crypto/rand"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestKnowledgeR30DatabaseIngressAndControls(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
		require.NoError(t, err)
		config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return failure }
		db, err := pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(db.Close)
		s := Service{DB: db}
		require.ErrorIs(t, s.knownActor(t.Context(), "alice"), core.DatabaseOperationError(failure))
		err = s.sourceReferenceCurrent(
			t.Context(),
			memoryReference{Namespace: MemoryShared, Topic: AssistantAbout, Key: "item"},
		)
		require.ErrorIs(t, err, core.DatabaseOperationError(failure))
		_, err = s.Proposals(t.Context(), "alice", ProposalQuery{})
		require.ErrorIs(t, err, core.DatabaseOperationError(failure))
		generation := int64(0)
		_, _, err = s.CommandReceipt(t.Context(), "alice", Command{Name: MemoDelete, Key: "operation", FactKey: "memo"},
			readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}})
		require.ErrorIs(t, err, core.DatabaseOperationError(failure))
		_, err = s.SearchMemory(t.Context(), "alice", MemoryQuery{Mode: MemoryRegex, Text: "["})
		require.Error(t, err)
		require.False(t, core.IsDatabaseFailure(err))
		_, err = s.ReadMemoryPage(t.Context(), "alice", "malformed", "")
		require.Error(t, err)
		require.False(t, core.IsDatabaseFailure(err))
	}
}

type r30DocumentRow struct{ err error }

func (row r30DocumentRow) Scan(...any) error { return row.err }

type r30DocumentTx struct {
	pgx.Tx

	readErr error
	written *bool
}

func (tx r30DocumentTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return r30DocumentRow{err: tx.readErr}
}

func (tx r30DocumentTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	*tx.written = true
	return pgconn.CommandTag{}, io.EOF
}

func TestKnowledgeR30DocumentWriteAndDomainControls(t *testing.T) {
	t.Parallel()
	command := Command{Name: DocumentSet, Topic: "notes", FactKey: "key", Text: "body"}
	written := false
	tx := r30DocumentTx{written: &written}
	_, err := writeDocument(t.Context(), tx, "alice", command)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, written)
	written = false
	tx.readErr = io.EOF
	_, err = writeDocument(t.Context(), tx, "alice", command)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.False(t, written)
	tx.readErr = pgx.ErrNoRows
	command.Name = DocumentDelete
	_, err = writeDocument(t.Context(), tx, "alice", command)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	require.False(t, written)
	command.Version = 1
	_, err = writeDocument(t.Context(), tx, "alice", command)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	require.False(t, written)
}

func r30Database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_knowledge_r30_" + strings.ToLower(rand.Text())
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
	return db
}

type r30SQLFault struct {
	prefix   string
	fired    bool
	closeErr error
}

func (f *r30SQLFault) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if !f.fired && strings.HasPrefix(data.SQL, f.prefix) {
		f.fired = true
		f.closeErr = conn.Close(ctx)
	}
	return ctx
}

func (*r30SQLFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestKnowledgeR30ReadTransportAfterAuthorization(t *testing.T) {
	t.Parallel()
	db := r30Database(t)
	for _, test := range []struct {
		name, prefix string
		call         func(Service) error
	}{
		{"fact", "SELECT f.body", func(s Service) error {
			_, err := s.Fact(t.Context(), "alice", "", "topic", "key")
			return err
		}},
		{"document", "SELECT version,active", func(s Service) error {
			_, err := s.DocumentState(t.Context(), "alice", "topic", "key")
			return err
		}},
		{"scopes", "WITH scopes", func(s Service) error {
			_, err := s.Scopes(t.Context(), "alice")
			return err
		}},
		{"search", "WITH candidates", func(s Service) error {
			_, err := s.SearchMemory(t.Context(), "alice", MemoryQuery{})
			return err
		}},
		{"deletions", "SELECT\n COALESCE", func(s Service) error {
			_, err := s.MemoryDeletions(t.Context(), "alice")
			return err
		}},
		{"proposal_commit", "commit", func(s Service) error {
			_, err := s.Proposals(t.Context(), "alice", ProposalQuery{})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fault := &r30SQLFault{prefix: test.prefix}
			config := db.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			err = test.call(Service{DB: broken})
			require.ErrorIs(t, err, core.ErrDatabase)
			require.EqualError(t, err, core.ErrDatabase.Error())
			require.True(t, fault.fired)
			require.NoError(t, fault.closeErr)
			require.NoError(t, test.call(Service{DB: db}))
		})
	}
	s := Service{DB: db}
	fact, err := s.Fact(t.Context(), "alice", "", "topic", "missing")
	require.NoError(t, err)
	require.Zero(t, fact.Version)
	document, err := s.DocumentState(t.Context(), "alice", "topic", "missing")
	require.NoError(t, err)
	require.Zero(t, document.Version)
	_, err = s.Scope(t.Context(), "alice", "missing")
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	_, err = s.ScopePage(t.Context(), "alice", "!")
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
}
