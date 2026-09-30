package bot

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const diagnosticsOmitted = "agent diagnostics omitted"

type pgAction int

const (
	// pgDrop closes the socket after the query arrives: an unlabelled transport EOF,
	// neither *pgconn.PgError nor *pgconn.ConnectError.
	pgDrop pgAction = iota
	// pgHang reads the query and never answers.
	pgHang
	// pgRows answers with a row description and the given text-format rows.
	pgRows
)

type pgReply struct {
	action pgAction
	oids   []uint32
	rows   [][][]byte
}

// fakePostgres speaks just enough of the wire protocol for one simple-protocol
// connection, so SQL-origin classification is exercised through the real driver.
type fakePostgres struct {
	replies []pgReply
	queries chan string
}

func sqlBot(t *testing.T, logger *slog.Logger, replies ...pgReply) (*Bot, <-chan string) {
	t.Helper()
	fake := &fakePostgres{replies: replies, queries: make(chan string, len(replies)+1)}
	config, err := pgxpool.ParseConfig("postgres://bot@fake-postgres/bot?sslmode=disable")
	require.NoError(t, err)
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	config.ConnConfig.LookupFunc = func(context.Context, string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go fake.serve(server)
		return client, nil
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return &Bot{DB: pool, Logger: logger}, fake.queries
}

func (f *fakePostgres) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	backend := pgproto3.NewBackend(conn, conn)
	if !acceptStartup(conn, backend) {
		return
	}
	for _, reply := range f.replies {
		message, err := backend.Receive()
		if err != nil {
			return
		}
		query, ok := message.(*pgproto3.Query)
		if !ok {
			return
		}
		select {
		case f.queries <- query.String:
		default:
		}
		switch reply.action {
		case pgDrop:
			return
		case pgHang:
			_, _ = backend.Receive()
			return
		case pgRows:
			if !sendRows(backend, reply) {
				return
			}
		}
	}
}

func acceptStartup(conn net.Conn, backend *pgproto3.Backend) bool {
	for {
		message, err := backend.ReceiveStartupMessage()
		if err != nil {
			return false
		}
		switch message.(type) {
		case *pgproto3.SSLRequest, *pgproto3.GSSEncRequest:
			// Decline encryption; the client continues in plaintext.
			if _, err = conn.Write([]byte("N")); err != nil {
				return false
			}
		case *pgproto3.StartupMessage:
			backend.Send(&pgproto3.AuthenticationOk{})
			backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
			backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			return backend.Flush() == nil
		default:
			return false
		}
	}
}

func sendRows(backend *pgproto3.Backend, reply pgReply) bool {
	fields := make([]pgproto3.FieldDescription, len(reply.oids))
	for i, oid := range reply.oids {
		fields[i] = pgproto3.FieldDescription{
			Name: []byte("c" + strconv.Itoa(i)), DataTypeOID: oid, DataTypeSize: -1, TypeModifier: -1,
		}
	}
	backend.Send(&pgproto3.RowDescription{Fields: fields})
	for _, row := range reply.rows {
		backend.Send(&pgproto3.DataRow{Values: row})
	}
	backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT " + strconv.Itoa(len(reply.rows)))})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return backend.Flush() == nil
}

func requireSanitizedDatabaseError(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
}

func TestOrderCallbackEventClassifiesLookupFailures(t *testing.T) {
	t.Parallel()
	textRow := func(value []byte) pgReply {
		return pgReply{action: pgRows, oids: []uint32{pgtype.TextOID}, rows: [][][]byte{{value}}}
	}
	cases := map[string]struct {
		text  string
		reply pgReply
	}{
		"order page transport loss":   {text: orderPagePrefix + "token", reply: pgReply{action: pgDrop}},
		"order button transport loss": {text: orderCallbackPrefix + "token", reply: pgReply{action: pgDrop}},
		"order button null event":     {text: orderCallbackPrefix + "token", reply: textRow(nil)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, _ := sqlBot(t, nil, tc.reply)
			_, err := b.orderCallbackEvent(t.Context(), incoming{owner: "owner-1", text: tc.text}, 1, "default-event")
			requireSanitizedDatabaseError(t, err)
		})
	}
}

func TestOrderCallbackEventKeepsExpectedOutcomes(t *testing.T) {
	t.Parallel()
	t.Run("unknown token keeps default", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil, pgReply{action: pgRows, oids: []uint32{pgtype.TextOID}})
		event, err := b.orderCallbackEvent(
			t.Context(), incoming{owner: "owner-1", text: orderPagePrefix + "missing"}, 1, "default-event",
		)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		require.False(t, core.IsDatabaseFailure(err))
		require.Equal(t, "default-event", event)
	})
	t.Run("stored token selects event", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil, pgReply{
			action: pgRows, oids: []uint32{pgtype.TextOID}, rows: [][][]byte{{[]byte("stored-event")}},
		})
		event, err := b.orderCallbackEvent(
			t.Context(), incoming{owner: "owner-1", text: orderPagePrefix + "token"}, 1, "default-event",
		)
		require.NoError(t, err)
		require.Equal(t, "stored-event", event)
	})
	t.Run("other callback needs no lookup", func(t *testing.T) {
		t.Parallel()
		event, err := (&Bot{}).orderCallbackEvent(
			t.Context(), incoming{owner: "owner-1", text: "select:slot:1"}, 1, "default-event",
		)
		require.NoError(t, err)
		require.Equal(t, "default-event", event)
	})
}

func TestDeliverCardLookupFailureIsSanitized(t *testing.T) {
	t.Parallel()
	b, _ := sqlBot(t, nil, pgReply{action: pgDrop})
	err := b.deliverCard(t.Context(), "owner-1", telegram.Send{ChatID: 1, Text: "card"})
	requireSanitizedDatabaseError(t, err)
}

func TestRecordMarshalFailureIsNotDatabaseFailure(t *testing.T) {
	t.Parallel()
	err := (&Bot{}).record(t.Context(), "owner-1", 1, "input", make(chan int))
	require.Error(t, err)
	require.NotErrorIs(t, err, core.ErrDatabase)
	require.False(t, core.IsDatabaseFailure(err))
}

func diagnosticRow(correlation, attempt []byte) pgReply {
	return pgReply{
		action: pgRows,
		oids:   []uint32{pgtype.TextOID, pgtype.Int8OID},
		rows:   [][][]byte{{correlation, attempt}},
	}
}

func TestStartAgentDiagnosticsPropagatesSQLFailure(t *testing.T) {
	t.Parallel()
	cases := map[string]pgReply{
		"transport loss":   {action: pgDrop},
		"null correlation": diagnosticRow(nil, []byte("1")),
		"invalid attempt":  diagnosticRow([]byte(uuid.NewString()), []byte("not-a-number")),
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			b, _ := sqlBot(t, slog.New(slog.NewJSONHandler(&logs, nil)), reply)
			parent := t.Context()
			ctx, span, err := b.startAgentDiagnostics(parent, "owner-1", 1)
			requireSanitizedDatabaseError(t, err)
			require.Nil(t, span)
			require.Equal(t, parent, ctx)
			require.NotContains(t, logs.String(), diagnosticsOmitted)
		})
	}
}

func TestStartAgentDiagnosticsInvalidContextIsOptional(t *testing.T) {
	t.Parallel()
	cases := map[string]pgReply{
		"non-uuid correlation": diagnosticRow([]byte("not-a-uuid"), []byte("1")),
		"zero attempt":         diagnosticRow([]byte(uuid.NewString()), []byte("0")),
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			b, _ := sqlBot(t, slog.New(slog.NewJSONHandler(&logs, nil)), reply)
			_, span, err := b.startAgentDiagnostics(t.Context(), "owner-1", 1)
			require.NoError(t, err)
			require.Nil(t, span)
			require.Contains(t, logs.String(), diagnosticsOmitted)
		})
	}
}

func TestStartAgentDiagnosticsStartsSpan(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	b, _ := sqlBot(t, slog.New(slog.NewJSONHandler(&logs, nil)), diagnosticRow([]byte(uuid.NewString()), []byte("2")))
	_, span, err := b.startAgentDiagnostics(t.Context(), "owner-1", 1)
	require.NoError(t, err)
	require.NotNil(t, span)
	span.Finish(nil)
	require.NotContains(t, logs.String(), diagnosticsOmitted)
}

func TestStartAgentDiagnosticsWithoutLoggerSkipsSQL(t *testing.T) {
	t.Parallel()
	parent := t.Context()
	ctx, span, err := (&Bot{}).startAgentDiagnostics(parent, "owner-1", 1)
	require.NoError(t, err)
	require.Nil(t, span)
	require.Equal(t, parent, ctx)
}

func TestStartAgentDiagnosticsOwnTimeoutIsOmitted(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	b, _ := sqlBot(t, slog.New(slog.NewJSONHandler(&logs, nil)), pgReply{action: pgHang})
	parent := t.Context()
	started := time.Now()
	_, span, err := b.startAgentDiagnostics(parent, "owner-1", 1)
	require.NoError(t, err)
	require.Nil(t, span)
	require.GreaterOrEqual(t, time.Since(started), diagnosticsTimeout)
	require.NoError(t, parent.Err())
	require.Contains(t, logs.String(), diagnosticsOmitted)
}

func TestStartAgentDiagnosticsParentCancellationPropagates(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	b, queries := sqlBot(t, slog.New(slog.NewJSONHandler(&logs, nil)), pgReply{action: pgHang})
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-queries:
		case <-time.After(diagnosticsTimeout / 2):
		}
		cancel()
	}()
	_, span, err := b.startAgentDiagnostics(parent, "owner-1", 1)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, core.ErrDatabase)
	require.False(t, core.IsDatabaseFailure(err))
	require.Nil(t, span)
	require.NotContains(t, logs.String(), diagnosticsOmitted)
}
