package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// errR29Fault marks the scripted SQL call that receives the injected driver failure.
var errR29Fault = errors.New("r29 scripted fault")

type r29Row struct {
	values []any
	err    error
}

func r29Values(values ...any) r29Row { return r29Row{values: values} }

func r29Err(err error) r29Row { return r29Row{err: err} }

type r29ScannedRow struct {
	row   r29Row
	fault error
}

func (r r29ScannedRow) Scan(dest ...any) error {
	if r.row.err != nil {
		if errors.Is(r.row.err, errR29Fault) {
			return r.fault
		}
		return r.row.err
	}
	if len(dest) != len(r.row.values) {
		return fmt.Errorf("r29 scan arity %d != %d", len(dest), len(r.row.values))
	}
	for i, value := range r.row.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

// r29Tx scripts only QueryRow/Exec. Any other pgx.Tx method hits the nil
// embedded interface, so an unexpected SQL path cannot pass silently.
type r29Tx struct {
	pgx.Tx

	t     testing.TB
	fault error
	rows  []r29Row
	execs []error
	sql   []string
}

func (tx *r29Tx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.sql = append(tx.sql, sql)
	if len(tx.rows) == 0 {
		tx.t.Errorf("r29: unexpected QueryRow %q", sql)
		return r29ScannedRow{row: r29Err(errors.New("r29 unexpected query"))}
	}
	row := tx.rows[0]
	tx.rows = tx.rows[1:]
	return r29ScannedRow{row: row, fault: tx.fault}
}

func (tx *r29Tx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.sql = append(tx.sql, sql)
	if len(tx.execs) == 0 {
		tx.t.Errorf("r29: unexpected Exec %q", sql)
		return pgconn.CommandTag{}, errors.New("r29 unexpected exec")
	}
	err := tx.execs[0]
	tx.execs = tx.execs[1:]
	if errors.Is(err, errR29Fault) {
		err = tx.fault
	}
	return pgconn.CommandTag{}, err
}

type r29Case struct {
	name  string
	site  string
	rows  []r29Row
	execs []error
	run   func(context.Context, pgx.Tx) error
}

func r29KnownSQLCases() []r29Case {
	ownedProposal := Result{Proposal: &Proposal{ID: 11, Owner: "r29-user", Event: "r29-event"}}
	derivedRecord := proposalCausalRecord{id: 12, derived: true, refs: []readsource.Authority{{
		Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.DerivedProposal, ProposalID: 9},
	}}}
	scope := []r29Row{r29Values("r29-event"), r29Values("r29-event")}
	return []r29Case{
		{"lock actor", "FROM core.users", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error { return lockActor(ctx, tx, "r29-user") }},
		{"lock scope event", "FROM core.events", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error { return lockScope(ctx, tx, "r29-event") }},
		{"lock scope insert", "INSERT INTO core.knowledge_scopes",
			[]r29Row{r29Values("r29-event")}, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error { return lockScope(ctx, tx, "r29-event") }},
		{"lock scope row", "FROM core.knowledge_scopes",
			[]r29Row{r29Values("r29-event"), r29Err(errR29Fault)}, []error{nil},
			func(ctx context.Context, tx pgx.Tx) error { return lockScope(ctx, tx, "r29-event") }},
		{"operation replay", "FROM core.knowledge_operations", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, _, err := replay(ctx, tx, "r29-user", "key", "hash")
				return err
			}},
		{"operation receipt", "INSERT INTO core.knowledge_operations", nil, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return saveKnowledgeOperation(ctx, tx, "r29-user", Command{Name: MemoSet},
					Result{Memo: &Memo{Key: "k", Version: 1}}, "key", "hash")
			}},
		{"operation audit", "INSERT INTO core.knowledge_audit", nil, []error{nil, errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return saveKnowledgeOperation(ctx, tx, "r29-user", Command{Name: MemoSet},
					Result{Memo: &Memo{Key: "k", Version: 1}}, "key", "hash")
			}},
		{"fact version", "FROM core.knowledge_facts", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := factVersion(ctx, tx, Command{Topic: "t", FactKey: "k"})
				return err
			}},
		{"curate write", "INSERT INTO core.knowledge_facts", []r29Row{r29Values(int64(0))}, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := curate(ctx, tx, Command{Name: Curate, Topic: "t", FactKey: "k", Text: "x"})
				return err
			}},
		{"curate phase", "FROM core.events", []r29Row{r29Values(int64(0)), r29Err(errR29Fault)}, []error{nil},
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := curate(
					ctx,
					tx,
					Command{Name: Curate, Event: "r29-event", Topic: "t", FactKey: "k", Text: "x"},
				)
				return err
			}},
		{"suggest capacity", "SELECT count(*) FROM core.knowledge_proposals", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := suggest(ctx, tx, "r29-user", Command{Name: Suggest, Topic: "t", FactKey: "k", Text: "x"})
				return err
			}},
		{"suggest insert", "INSERT INTO core.knowledge_proposals",
			[]r29Row{r29Values(0), r29Values(int64(0)), r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := suggest(ctx, tx, "r29-user", Command{Name: Suggest, Topic: "t", FactKey: "k", Text: "x"})
				return err
			}},
		{"read proposal", "FROM core.knowledge_proposals WHERE id=$1 AND scope=$2", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := readProposal(ctx, tx, 1, "")
				return err
			}},
		{"update proposal", "UPDATE core.knowledge_proposals", nil, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := updateProposal(ctx, tx, Proposal{ID: 1}, "reason")
				return err
			}},
		{"memo version", "SELECT version FROM core.knowledge_memos", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := writeMemo(ctx, tx, "r29-user", Command{Name: MemoSet, FactKey: "k", Text: "x"})
				return err
			}},
		{"memo capacity", "SELECT count(*) FROM core.knowledge_memos",
			[]r29Row{r29Err(pgx.ErrNoRows), r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := writeMemo(ctx, tx, "r29-user", Command{Name: MemoSet, FactKey: "k", Text: "x"})
				return err
			}},
		{"memo write", "INSERT INTO core.knowledge_memos",
			[]r29Row{r29Err(pgx.ErrNoRows), r29Values(0)}, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := writeMemo(ctx, tx, "r29-user", Command{Name: MemoSet, FactKey: "k", Text: "x"})
				return err
			}},
		{"memory causal read", "FROM core.memory_revisions r", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := loadMemoryCausal(ctx, tx, "r29-user", MemoryEntry{Namespace: MemoryPrivate}, true)
				return err
			}},
		{"memory causal revoke", "UPDATE core.memory_read_authorities", nil, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return revokeMemoryCausal(ctx, tx, "r29-user", MemoryEntry{Namespace: MemoryPrivate})
			}},
		{"proposal causal read", "LEFT JOIN core.knowledge_proposal_authorities", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := loadProposalCausal(ctx, tx, 1)
				return err
			}},
		{"proposal causal lock", "FOR UPDATE", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := authorizeProposalRecord(ctx, tx, 12, derivedRecord, nil, readsource.Validity{})
				return err
			}},
		{"proposal causal revoke", "SET revoked=true", []r29Row{r29Values(false)}, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := authorizeProposalRecord(ctx, tx, 12, derivedRecord, nil, readsource.Validity{})
				return err
			}},
		{"replay proposal revoke", "SET revoked=true", nil, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return revokeMemoryResult(ctx, tx, "r29-user", Result{Proposal: &Proposal{ID: 12}})
			}},
		{"proposal causal bind", "SET origin='derived'", nil, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return bindProposalCausal(ctx, tx, &Proposal{ID: 13}, []readsource.Authority{})
			}},
		{"proposal source bind", "INSERT INTO core.memory_proposal_sources", nil, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return bindMemorySources(ctx, tx, "r29-user", ownedProposal, []int64{7})
			}},
		{"source resolve", "FROM core.conversation_events", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := resolveMemorySources(ctx, tx, "r29-user", []string{"source-1"})
				return err
			}},
		{"attach proposal count", "SELECT count(*) FROM core.memory_proposal_sources",
			append(append([]r29Row{}, scope...), r29Err(errR29Fault)), []error{nil},
			func(ctx context.Context, tx pgx.Tx) error {
				return attachProposalSources(ctx, tx, "r29-user", ownedProposal, nil)
			}},
		{"attach proposal link", "INSERT INTO core.memory_sources",
			append(append([]r29Row{}, scope...), r29Values(0)), []error{nil, errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				return attachProposalSources(ctx, tx, "r29-user", ownedProposal, nil)
			}},
		{"consent read", "SELECT proposal_version", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := recordProposalConsent(ctx, tx, Proposal{ID: 1}, Submission{Version: 1})
				return err
			}},
		{"consent write", "INSERT INTO core.knowledge_proposal_submissions",
			[]r29Row{r29Err(pgx.ErrNoRows)}, []error{errR29Fault},
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := recordProposalConsent(ctx, tx, Proposal{ID: 1, Version: 1, State: AwaitingSubmission},
					Submission{Version: 1})
				return err
			}},
		{"private deletion current", "FROM core.knowledge_memos", []r29Row{r29Err(errR29Fault)}, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := currentPrivateDeletion(ctx, tx, "r29-user", Command{Name: MemoDelete, FactKey: "k"})
				return err
			}},
	}
}

type r29FaultCase struct {
	name  string
	err   error
	check func(*testing.T, error)
}

func r29RequireSanitized(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "r29-private")
	var driver *pgconn.PgError
	require.NotErrorAs(t, err, &driver)
	require.NotErrorIs(t, err, io.EOF)
}

func r29Faults() []r29FaultCase {
	return []r29FaultCase{
		{"transport EOF", &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}, func(t *testing.T, err error) {
			t.Helper()
			r29RequireSanitized(t, err)
			require.EqualError(t, err, core.ErrDatabase.Error())
		}},
		{"statement", &pgconn.PgError{Code: "P0001", Message: "r29-private diagnostic"}, func(t *testing.T, err error) {
			t.Helper()
			r29RequireSanitized(t, err)
			require.EqualError(t, err, core.ErrDatabase.Error())
		}},
		{"serialization", fmt.Errorf("r29: %w", &pgconn.PgError{Code: "40001", Message: "r29-private"}),
			func(t *testing.T, err error) {
				t.Helper()
				r29RequireSanitized(t, err)
				require.ErrorIs(t, err, core.ErrDatabaseSerialization)
			}},
		{"cancellation", fmt.Errorf("r29 read: %w", context.Canceled), func(t *testing.T, err error) {
			t.Helper()
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, core.IsDatabaseFailure(err))
		}},
	}
}

// Each fault lands on the named statement, which must be the last SQL issued.
func TestR29KnownSQLOriginsMarkDriverFailures(t *testing.T) {
	t.Parallel()
	for _, test := range r29KnownSQLCases() {
		for _, fault := range r29Faults() {
			t.Run(test.name+"/"+fault.name, func(t *testing.T) {
				t.Parallel()
				tx := &r29Tx{t: t, fault: fault.err, rows: test.rows, execs: test.execs}
				err := test.run(t.Context(), tx)
				require.Error(t, err)
				fault.check(t, err)
				require.Empty(t, tx.rows, "every scripted row is consumed before the fault")
				require.Empty(t, tx.execs, "every scripted exec is consumed before the fault")
				require.NotEmpty(t, tx.sql)
				require.Contains(t, tx.sql[len(tx.sql)-1], test.site)
			})
		}
	}
}

func r29RequireUnmarked(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
}

func r29RequireProblem(t *testing.T, err error) {
	t.Helper()
	r29RequireUnmarked(t, err)
	_, ok := errors.AsType[*core.ProblemError](err)
	require.True(t, ok, "expected domain problem, got %v", err)
}

func r29RequireJSON(t *testing.T, err error) {
	t.Helper()
	r29RequireUnmarked(t, err)
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
}

func TestR29NoRowsAndDomainOutcomesStayUnmarked(t *testing.T) {
	t.Parallel()
	script := func(t *testing.T, rows ...r29Row) *r29Tx { return &r29Tx{t: t, rows: rows} }

	t.Run("missing actor is forbidden", func(t *testing.T) {
		t.Parallel()
		r29RequireProblem(t, lockActor(t.Context(), script(t, r29Err(pgx.ErrNoRows)), "r29-user"))
	})
	t.Run("missing event scope", func(t *testing.T) {
		t.Parallel()
		r29RequireProblem(t, lockScope(t.Context(), script(t, r29Err(pgx.ErrNoRows)), "r29-event"))
	})
	t.Run("missing general scope row keeps NoRows", func(t *testing.T) {
		t.Parallel()
		err := lockScope(t.Context(), script(t, r29Err(pgx.ErrNoRows)), "")
		require.Same(t, pgx.ErrNoRows, err)
	})
	t.Run("replay absence executes", func(t *testing.T) {
		t.Parallel()
		_, found, err := replay(t.Context(), script(t, r29Err(pgx.ErrNoRows)), "r29-user", "key", "hash")
		require.NoError(t, err)
		require.False(t, found)
	})
	t.Run("replay request mismatch conflicts", func(t *testing.T) {
		t.Parallel()
		_, _, err := replay(t.Context(), script(t, r29Values("other", []byte(`{}`))), "r29-user", "key", "hash")
		r29RequireProblem(t, err)
	})
	t.Run("replay decodes stored receipt", func(t *testing.T) {
		t.Parallel()
		_, found, err := replay(t.Context(), script(t, r29Values("hash", []byte(`{}`))), "r29-user", "key", "hash")
		require.NoError(t, err)
		require.True(t, found)
	})
	t.Run("corrupt replay JSON is not database", func(t *testing.T) {
		t.Parallel()
		_, _, err := replay(t.Context(), script(t, r29Values("hash", []byte(`{`))), "r29-user", "key", "hash")
		r29RequireJSON(t, err)
	})
	t.Run("missing fact is version zero", func(t *testing.T) {
		t.Parallel()
		version, err := factVersion(t.Context(), script(t, r29Err(pgx.ErrNoRows)), Command{})
		require.NoError(t, err)
		require.Zero(t, version)
	})
	t.Run("missing proposal", func(t *testing.T) {
		t.Parallel()
		_, err := readProposal(t.Context(), script(t, r29Err(pgx.ErrNoRows)), 1, "")
		r29RequireProblem(t, err)
	})
	t.Run("stale memo", func(t *testing.T) {
		t.Parallel()
		_, err := writeMemo(
			t.Context(),
			script(t, r29Values(int64(3))),
			"r29-user",
			Command{Name: MemoSet, FactKey: "k"},
		)
		r29RequireProblem(t, err)
	})
	t.Run("missing source", func(t *testing.T) {
		t.Parallel()
		_, err := resolveMemorySources(t.Context(), script(t, r29Err(pgx.ErrNoRows)), "r29-user", []string{"source-1"})
		r29RequireProblem(t, err)
	})
	t.Run("repeated consent at another version is stale", func(t *testing.T) {
		t.Parallel()
		_, err := recordProposalConsent(t.Context(), script(t, r29Values(int64(2))),
			Proposal{ID: 1, Submitted: true}, Submission{Version: 1})
		r29RequireProblem(t, err)
	})
}

func TestR29CausalJSONAndDenialStayUnmarked(t *testing.T) {
	t.Parallel()
	script := func(t *testing.T, rows ...r29Row) *r29Tx { return &r29Tx{t: t, rows: rows} }
	entry := MemoryEntry{Namespace: MemoryPrivate, Topic: "t", Key: "k", Version: 1}

	t.Run("absent memory revision", func(t *testing.T) {
		t.Parallel()
		record, err := loadMemoryCausal(t.Context(), script(t, r29Err(pgx.ErrNoRows)), "r29-user", entry, false)
		require.NoError(t, err)
		require.False(t, record.found)
	})
	t.Run("original memory without authority", func(t *testing.T) {
		t.Parallel()
		record, err := loadMemoryCausal(t.Context(), script(t, r29Values([]byte(nil), false, "original")),
			"r29-user", entry, false)
		require.NoError(t, err)
		require.False(t, record.found)
	})
	t.Run("derived memory null authority", func(t *testing.T) {
		t.Parallel()
		_, err := loadMemoryCausal(t.Context(), script(t, r29Values([]byte(nil), false, "derived")),
			"r29-user", entry, false)
		r29RequireUnmarked(t, err)
	})
	t.Run("corrupt memory authority JSON", func(t *testing.T) {
		t.Parallel()
		_, err := loadMemoryCausal(t.Context(), script(t, r29Values([]byte(`{`), false, "derived")),
			"r29-user", entry, false)
		r29RequireJSON(t, err)
	})
	t.Run("absent proposal", func(t *testing.T) {
		t.Parallel()
		record, err := loadProposalCausal(t.Context(), script(t, r29Err(pgx.ErrNoRows)), 1)
		require.NoError(t, err)
		require.False(t, record.derived)
	})
	t.Run("corrupt proposal authority JSON", func(t *testing.T) {
		t.Parallel()
		_, err := loadProposalCausal(t.Context(), script(t, r29Values("derived", []byte(`[`), false, false)), 1)
		r29RequireJSON(t, err)
	})
	t.Run("revoked proposal is a boolean denial", func(t *testing.T) {
		t.Parallel()
		tx := script(t, r29Values(true))
		allowed, err := authorizeProposalRecord(t.Context(), tx, 1, proposalCausalRecord{id: 1, derived: true},
			nil, readsource.Validity{})
		require.NoError(t, err)
		require.False(t, allowed)
		require.Len(t, tx.sql, 1, "an already revoked valid origin issues no revocation write")
	})
	t.Run("unsupported deletion issues no SQL", func(t *testing.T) {
		t.Parallel()
		tx := script(t)
		current, err := currentPrivateDeletion(t.Context(), tx, "r29-user", Command{Name: MemoSet})
		require.NoError(t, err)
		require.False(t, current)
		require.Empty(t, tx.sql)
	})
}

func r29UnreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=r29-private-user dbname=r29-private-db sslmode=disable")
	require.NoError(t, err)
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// Pool-level Begin/Query origins: the raw pgconn.ConnectError would already be a
// database failure, so the positive proof is sanitization of its diagnostics.
func TestR29ServicePoolOriginsSanitizeAndKeepCancellation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		run  func(context.Context, Service) error
	}{
		{"assess read", func(ctx context.Context, s Service) error {
			_, err := s.Assess(ctx, "r29-user", Assessment{Key: "r29", ProposalID: 1, Version: 1})
			return err
		}},
		{"submit begin", func(ctx context.Context, s Service) error {
			_, err := s.SubmitProposal(ctx, "r29-user",
				Submission{ProposalID: 1, Version: 1, Topic: "topic", FactKey: "key", Text: "text"})
			return err
		}},
		{"memory causal begin", func(ctx context.Context, s Service) error {
			_, err := s.authorizeMemoryEntries(ctx, "r29-user", nil)
			return err
		}},
		{"proposal causal begin", func(ctx context.Context, s Service) error {
			_, err := s.authorizeProposals(ctx, "r29-user", nil)
			return err
		}},
		{"attach sources begin", func(ctx context.Context, s Service) error {
			return s.AttachMemorySources(ctx, "r29-user", "r29-operation", nil)
		}},
		{"derived topic query", func(ctx context.Context, s Service) error {
			_, err := s.derivedTopicEntries(ctx, "r29-user", MemoryQuery{}, MemoryTopic{})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := Service{DB: r29UnreachablePool(t)}
			err := test.run(t.Context(), s)
			r29RequireSanitized(t, err)
			require.EqualError(t, err, core.ErrDatabase.Error())
			var connect *pgconn.ConnectError
			require.NotErrorAs(t, err, &connect)

			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			err = test.run(canceled, s)
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, core.IsDatabaseFailure(err))
		})
	}
}
