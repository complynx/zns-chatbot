package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptSQLTrace struct {
	statement  string
	hit        atomic.Bool
	parentLive atomic.Bool
	updates    atomic.Int64
}

func (trace *scriptSQLTrace) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	if strings.HasPrefix(statement, "UPDATE bot.interactions SET content=") {
		trace.updates.Add(1)
	}
	if statement != trace.statement || !trace.hit.CompareAndSwap(false, true) {
		return ctx
	}
	trace.parentLive.Store(ctx.Err() == nil)
	// Expire only the driver's query context; the public API request stays live.
	local, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	cancel()
	return local
}

func (*scriptSQLTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type scriptSQLWorker struct{ calls atomic.Int64 }

func (worker *scriptSQLWorker) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	worker.calls.Add(1)
	return json.RawMessage(`null`), nil
}

func scriptSQLPool(t *testing.T, f *fixture, trace *scriptSQLTrace) *pgxpool.Pool {
	t.Helper()
	config := f.db.Config().Copy()
	config.ConnConfig.Tracer = trace
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

func TestScriptReservationLocalSQLDeadlineStopsWorker(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{
		"snapshot": "SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3",
		"begin":    "begin",
		"lock":     "SELECT pg_advisory_xact_lock(hashtextextended($1,0))",
		"locked snapshot": "SELECT content FROM bot.interactions " +
			"WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE",
		"write": "INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) " +
			"ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content",
		"commit": "commit",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			anchor := seedScriptSQLLedger(t, f, 909, scriptSQLRecords())
			trace := &scriptSQLTrace{statement: statement}
			worker := &scriptSQLWorker{}
			policy := liveLedgerAuthority{fixture: f}
			generation, err := policy.Generation(t.Context(), "alice")
			require.NoError(t, err)
			host := agenthost.ScriptHost{
				Store: agenthost.ScriptStore{DB: scriptSQLPool(t, f, trace), Policy: policy}, Worker: worker,
			}
			err = host.Perform(t.Context(), "alice", 910,
				agent.ScriptProposal{Code: "return null;", InputJSON: "null"},
				&agent.Input{HistoryGeneration: generation, Script: &agent.ScriptContext{Remaining: 1}})
			requireScriptLocalSQL(t, trace, err)
			require.Zero(t, worker.calls.Load(), "no worker or tool effect is admitted after failed reservation")
			require.JSONEq(t, anchor, readScriptSQLLedger(t, f, 909))
			var reservations int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions
WHERE owner='alice' AND update_id=910 AND kind='script_runs'`).Scan(&reservations))
			if name == "commit" {
				// A deadline before sending COMMIT is not an ambiguous-acknowledgement simulation.
				// Inspect storage without inferring rollback from the returned error alone.
				require.LessOrEqual(t, reservations, 1)
			} else {
				require.Zero(t, reservations)
			}
		})
	}
}

func TestScriptRetirementLocalSQLDeadlinePreservesCause(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{
		"begin": "begin",
		"lock":  "SELECT pg_advisory_xact_lock(hashtextextended($1,0))",
		"locked snapshot": "SELECT content FROM bot.interactions " +
			"WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE",
		"write":  "UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3",
		"commit": "commit",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			before := seedScriptSQLLedger(t, f, 910, scriptSQLRecords())
			anchor := seedScriptSQLLedger(t, f, 909, scriptSQLRecords())
			original := errors.New("synthetic retirement authority interruption")
			policy := &retiringLedgerAuthority{retired: true, cause: original}
			policy.fixture = f
			trace := &scriptSQLTrace{statement: statement}
			store := agenthost.ScriptStore{
				DB: scriptSQLPool(t, f, trace), Policy: policy, StaleError: appclient.ErrReadStale,
			}
			_, err := store.LoadAuthorized(t.Context(), "alice", 910)
			requireScriptLocalSQL(t, trace, err)
			require.ErrorIs(t, err, original, "repair failure must preserve the original authority cause")
			require.JSONEq(t, anchor, readScriptSQLLedger(t, f, 909))
			stored := readScriptSQLLedger(t, f, 910)
			if name != "commit" {
				require.JSONEq(t, before, stored, "failed transaction cannot publish partial retirement")
			}
			var records []agenthost.ScriptRecord
			require.NoError(t, json.Unmarshal([]byte(stored), &records))
			require.Len(t, records, 1)
			require.Len(t, records[0].Calls, 1)
			require.NotNil(t, records[0].Calls[0].Memory)
			require.Equal(t, "retained-script-effect", records[0].Calls[0].Memory.Key)
		})
	}
}

func TestScriptRetirementStoredJSONKeepsDecodeErrorsNonSQL(t *testing.T) {
	t.Parallel()
	for _, shape := range []string{"null", "[]", "{}", `"incompatible"`, "records"} {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			seedScriptSQLLedger(t, f, 910, scriptSQLRecords())
			anchor := seedScriptSQLLedger(t, f, 909, scriptSQLRecords())
			replacement := shape
			var appended agenthost.ScriptRecord
			if shape == "records" {
				records := scriptSQLRecords()
				appended = agenthost.ScriptRecord{
					HistoryGeneration: 77, Request: agent.ScriptProposal{Code: "return 'independent';"},
					Run: agent.ScriptRun{Error: "interrupted"},
				}
				raw, err := json.Marshal(append(records, appended))
				require.NoError(t, err)
				replacement = string(raw)
			}
			original := errors.New("synthetic retirement observation")
			var swaps atomic.Int64
			policy := &retiringLedgerAuthority{retired: true, cause: original}
			policy.fixture = f
			policy.after = func(ctx context.Context, _ agenthost.ScriptRecord) error {
				swaps.Add(1)
				_, err := f.db.Exec(ctx, `UPDATE bot.interactions SET content=$1::jsonb
WHERE owner='alice' AND update_id=910 AND kind='script_runs'`, replacement)
				return err
			}
			trace := &scriptSQLTrace{}
			store := agenthost.ScriptStore{DB: scriptSQLPool(t, f, trace), Policy: policy}
			_, err := store.LoadAuthorized(t.Context(), "alice", 910)
			require.ErrorIs(t, err, original)
			require.False(t, core.IsDatabaseFailure(err), "JSON decoding is separate from successful SQL Scan")
			require.NoError(t, t.Context().Err())
			require.Equal(t, int64(1), swaps.Load(), "replacement occurs after detached observation, before repair")
			require.JSONEq(t, anchor, readScriptSQLLedger(t, f, 909))
			stored := readScriptSQLLedger(t, f, 910)
			var decodeError *json.UnmarshalTypeError
			if shape == "{}" || shape == `"incompatible"` {
				require.ErrorAs(t, err, &decodeError)
				require.Zero(t, trace.updates.Load(), "invalid shape must not execute the retirement UPDATE")
				require.JSONEq(t, replacement, stored)
				return
			}
			require.NotErrorAs(t, err, &decodeError)
			require.Equal(t, int64(1), trace.updates.Load())
			if shape != "records" {
				require.JSONEq(t, shape, stored)
				return
			}
			var records []agenthost.ScriptRecord
			require.NoError(t, json.Unmarshal([]byte(stored), &records))
			require.Len(t, records, 2)
			require.True(t, records[0].PassRedacted)
			require.Empty(t, records[0].Request.Code)
			require.Len(t, records[0].Calls, 1)
			require.NotNil(t, records[0].Calls[0].Memory)
			require.Empty(t, records[0].Calls[0].Memory.Text)
			require.Equal(t, "retained-script-effect", records[0].Calls[0].Memory.Key)
			require.Equal(t, appended, records[1], "an unobserved independent admission must remain unchanged")
		})
	}
}

func requireScriptLocalSQL(t *testing.T, trace *scriptSQLTrace, err error) {
	t.Helper()
	require.True(t, trace.hit.Load(), "the selected real pgx operation must reach its injected deadline")
	require.True(t, trace.parentLive.Load())
	require.NoError(t, t.Context().Err())
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotErrorIs(t, err, context.DeadlineExceeded, "driver-local cancellation is sanitized as SQL")
	require.NotContains(t, err.Error(), "context deadline exceeded")
}

func scriptSQLRecords() []agenthost.ScriptRecord {
	return []agenthost.ScriptRecord{{
		Request: agent.ScriptProposal{Code: "return 'private';", InputJSON: "null"},
		Run:     agent.ScriptRun{Error: "interrupted"},
		Calls: []agenthost.ScriptToolRecord{{
			Memory:  &knowledge.Command{Name: knowledge.MemoSet, Key: "retained-script-effect", Text: "private body"},
			Outcome: agent.ScriptToolResult{Name: "memory.set", Result: json.RawMessage(`{"saved":true}`)},
		}},
	}}
}

func seedScriptSQLLedger(t *testing.T, f *fixture, id int64, records []agenthost.ScriptRecord) string {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('alice',$1,'script_runs',$2)`, id, records)
	require.NoError(t, err)
	return readScriptSQLLedger(t, f, id)
}

func readScriptSQLLedger(t *testing.T, f *fixture, id int64) string {
	t.Helper()
	var raw string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions
WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, id).Scan(&raw))
	return raw
}
