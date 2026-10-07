package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestAgentHostChoiceParentAvailability(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	store := agenthost.ScriptStore{DB: f.db, StaleError: appclient.ErrReadStale}
	cases := []string{"available", "history", "pass", "memory", "null", "error", "expired"}
	for number, state := range cases {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			update := int64(45900 + number)
			ref := fmt.Sprintf("%d.0.0", update)
			generation := int64(0)
			parent := agenthost.ScriptRecord{HistoryGeneration: generation, Calls: []agenthost.ScriptToolRecord{{
				ModernChoice: &agenthost.ModernChoiceRecord{Ref: ref, HistoryGeneration: &generation},
				Outcome:      agent.ScriptToolResult{Result: json.RawMessage(`{"accepted":true}`)},
			}}}
			switch state {
			case "history":
				parent.HistoryRedacted = true
			case "pass":
				parent.PassRedacted = true
			case "memory":
				parent.MemoryRedacted = true
			case "null":
				parent.Calls[0].Outcome.Result = json.RawMessage(`null`)
			case "error":
				parent.Calls[0].Outcome.Error = "unavailable"
			}
			created := time.Now()
			if state == "expired" {
				created = created.Add(-25 * time.Hour)
			}
			_, err := f.db.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content,created_at)
 VALUES('bob',$1,'script_runs',$2,$3)`, update, []agenthost.ScriptRecord{parent}, created)
			require.NoError(t, err)
			_, loadErr := store.LoadModernChoice(ctx, "bob", ref)
			tx, err := f.db.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			claimErr := store.ClaimModernChoice(ctx, tx, "bob", update+100, nil, ref, "child", generation)
			if state == "available" {
				require.NoError(t, loadErr)
				require.NoError(t, claimErr)
				require.NoError(t, tx.Commit(ctx))
				_, err = store.LoadModernChoice(ctx, "bob", ref)
				require.Error(t, err, "successful cross-update claim consumes the parent")
			} else {
				require.Error(t, loadErr, "unavailable parent must not be hydrated")
				require.Error(t, claimErr, "unavailable parent must not be claimed")
				require.NoError(t, tx.Rollback(ctx))
				var child string
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT COALESCE(content->0->'calls'->0->'modern_choice'->>'child','')
 FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, update).Scan(&child),
				)
				require.Empty(t, child)
			}
		})
	}
}

type liveLedgerAuthority struct {
	fixture *fixture
	after   func(context.Context, agenthost.ScriptRecord) error
}

type retiringLedgerAuthority struct {
	liveLedgerAuthority

	retired bool
	cause   error
}

func TestAgentHostLedgerAuthorizedReadIsReadOnly(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	links := identity.Links{DB: f.db, Issuer: "https://ledger.test", BotID: 1}
	require.NoError(t, links.Bind(ctx, "bob", identity.BobTelegramID, "ledger-bob"))
	f.b.Host.UserToken = func(ctx context.Context, owner string) (string, error) {
		resolved, authErr := links.Subject(ctx, "ledger-bob")
		if authErr != nil {
			return "", authErr
		}
		if resolved != owner {
			return "", identity.ErrZitadelIdentity
		}
		return f.b.API.UserToken(ctx, owner)
	}
	policy := &liveLedgerAuthority{fixture: f}
	store := agenthost.ScriptStore{DB: f.db, Policy: policy, StaleError: appclient.ErrReadStale}
	const update = 48900
	generation, err := policy.Generation(ctx, "bob")
	require.NoError(t, err)
	_, err = store.ReserveRun(ctx, "bob", update, agent.ScriptProposal{Code: "return null;"},
		generation, nil, nil, false)
	require.NoError(t, err)
	config := f.db.Config().Copy()
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	config.MaxConns = 2
	reader, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(reader.Close)
	var readOnly string
	require.NoError(t, reader.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&readOnly))
	require.Equal(t, "on", readOnly)
	store.DB = reader
	loaded, err := store.LoadAuthorized(ctx, "bob", update)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, "return null;", loaded[0].Request.Code)

	injected, approvedRevision := false, false
	policy.after = func(ctx context.Context, record agenthost.ScriptRecord) error {
		if string(record.Run.Result) == `"new revision"` {
			approvedRevision = true
		}
		if injected {
			return nil
		}
		injected = true
		record.Run.Result = json.RawMessage(`"new revision"`)
		_, changeErr := f.db.Exec(
			ctx,
			`UPDATE bot.interactions SET content=$3 WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`,
			"bob",
			update,
			[]agenthost.ScriptRecord{record},
		)
		return changeErr
	}
	loaded, err = store.LoadAuthorized(ctx, "bob", update)
	require.NoError(t, err)
	require.True(t, approvedRevision, "returned revision must pass its own fresh policy decision")
	require.Len(t, loaded, 1)
	require.JSONEq(t, `"new revision"`, string(loaded[0].Run.Result))
	policy.after = nil
	_, err = f.db.Exec(ctx, `UPDATE core.zitadel_identities SET active=false WHERE owner='bob'`)
	require.NoError(t, err)
	var active bool
	require.NoError(t, f.db.QueryRow(ctx, `SELECT active FROM core.zitadel_identities WHERE owner='bob'`).Scan(&active))
	require.False(t, active)
	loaded, err = store.LoadAuthorized(ctx, "bob", update)
	require.Error(t, err, "inactive identity must not receive authorized ledger data")
	require.Empty(t, loaded)
}

func (policy *retiringLedgerAuthority) AccessChanged(
	ctx context.Context,
	owner string,
	record agenthost.ScriptRecord,
) (bool, error) {
	_, err := policy.liveLedgerAuthority.AccessChanged(ctx, owner, record)
	if policy.retired {
		err = errors.Join(err, policy.cause)
	}
	return policy.retired, err
}

func TestAgentHostRetirementRepairPreservesOriginalFailure(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	policy := &retiringLedgerAuthority{}
	policy.fixture = f
	store := agenthost.ScriptStore{DB: f.db, Policy: policy, StaleError: appclient.ErrReadStale}
	generation, err := policy.Generation(ctx, "bob")
	require.NoError(t, err)
	_, err = store.ReserveRun(
		ctx,
		"bob",
		47900,
		agent.ScriptProposal{Code: "return null;"},
		generation,
		nil,
		nil,
		false,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(ctx, `CREATE FUNCTION bot.reject_test_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'synthetic retirement write failure' USING ERRCODE='P0001'; END $$;
 CREATE TRIGGER reject_test_retirement BEFORE UPDATE ON bot.interactions
 FOR EACH ROW WHEN (OLD.owner='bob' AND OLD.update_id=47900 AND OLD.kind='script_runs')
 EXECUTE FUNCTION bot.reject_test_retirement()`)
	require.NoError(t, err)
	original := errors.New("synthetic authorization outage after retirement")
	policy.retired, policy.cause = true, original
	_, err = store.LoadAuthorized(ctx, "bob", 47900)
	require.ErrorIs(t, err, original, "repair failure must not replace the original cause")
	// The failed retirement UPDATE is joined as a sanitized database failure.
	require.ErrorIs(t, err, core.ErrDatabase, "repair failure must remain visible")
	require.True(t, core.IsDatabaseFailure(err))
	require.NotErrorIs(t, err, core.ErrDatabaseSerialization)
	var databaseError *pgconn.PgError
	require.NotErrorAs(t, err, &databaseError, "driver diagnostics must not escape")
	require.NotContains(t, err.Error(), "synthetic retirement write failure")
}

func TestAgentHostRetirementScrubsAdmittedPrivateCarriers(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	const owner = "bob"
	const canary = "private-carrier-canary"
	cases := []string{
		`{"memory":{"name":"memo_set","text":"private-carrier-canary"}}`,
		`{"pass":{"id":"assignment","name":"passes.admin.assign","assignment":{"event":"dance","target":"alice","target_version":1,"comment":"private-carrier-canary","create":{"role":"leader","legal_name":"private-carrier-canary"}}}}`,
		`{"pass":{"id":"batch","name":"passes.batch.assign","batch":{"event":"dance","action":"admin_assign","recipients":[101],"options":{"comment":"private-carrier-canary","create":{"role":"leader","legal_name":"private-carrier-canary"}}}}}`,
		`{"order":{"name":"create","country":"private-carrier-canary","choice":{"customer":"private-carrier-canary","customer_first_name":"private-carrier-canary","customer_last_name":"private-carrier-canary","customer_patronymus":"private-carrier-canary"}}}`,
		`{"modern_choice":{"ref":"choice","event":"dance","history_generation":0,"choice":{"customer":"private-carrier-canary","customer_first_name":"private-carrier-canary"}}}`,
		`{"broadcast":{"command":"private-carrier-canary","key":"broadcast-key"}}`,
	}
	for index, raw := range cases {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()
			policy := &retiringLedgerAuthority{}
			policy.fixture = f
			store := agenthost.ScriptStore{DB: f.db, Policy: policy, StaleError: appclient.ErrReadStale}
			update := int64(46900 + index)
			generation, err := policy.Generation(ctx, owner)
			require.NoError(t, err)
			run, err := store.ReserveRun(
				ctx,
				owner,
				update,
				agent.ScriptProposal{Code: canary},
				generation,
				nil,
				nil,
				false,
			)
			require.NoError(t, err)
			var call agenthost.ScriptToolRecord
			require.NoError(t, json.Unmarshal([]byte(raw), &call))
			call.Outcome.Name = "carrier"
			if call.ModernChoice != nil {
				call.ModernChoice.HistoryGeneration = &generation
			}
			_, err = store.AdmitCall(ctx, owner, update, run, &call)
			require.NoError(t, err)
			var witness *passbooking.OperationWitness
			if call.Pass != nil {
				witness = call.Pass.Witness
				require.NotNil(t, witness)
				require.True(t, witness.Valid(owner))
			}
			policy.retired = true
			records, err := store.LoadAuthorized(ctx, owner, update)
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.True(t, records[0].PassRedacted)
			var persisted json.RawMessage
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`, owner, update).
					Scan(&persisted),
			)
			require.NotContains(t, string(persisted), canary)
			if witness != nil {
				require.Equal(t, witness, records[0].Calls[0].Pass.Witness)
				_, _, err = store.ReadRegistrationOperation(ctx, owner, call.Pass.ID)
				require.Error(t, err, "terminal admission must never become executable from its witness")
			}
		})
	}
}

func (policy liveLedgerAuthority) Generation(ctx context.Context, owner string) (int64, error) {
	return policy.fixture.b.API.HistoryGeneration(ctx, owner)
}

func (policy liveLedgerAuthority) MemoryState(
	ctx context.Context,
	owner string,
) (knowledge.MemoryDeletionState, error) {
	return policy.fixture.b.API.MemoryDeletions(ctx, owner)
}

func (policy liveLedgerAuthority) AccessChanged(
	ctx context.Context,
	owner string,
	record agenthost.ScriptRecord,
) (bool, error) {
	refs, err := agenthost.ScriptReadAuthorities(owner, []agenthost.ScriptRecord{record})
	if err == nil {
		err = policy.fixture.b.Host.CheckReadAuthorities(ctx, owner, refs)
	}
	if err == nil && policy.after != nil {
		err = policy.after(ctx, record)
	}
	return false, err
}

func ledgerAdmission(name string) agenthost.ScriptToolRecord {
	return agenthost.ScriptToolRecord{
		Pass: &agenthost.ScriptPassRequest{ID: name, Name: "passes.admin.cancel", Command: &passbooking.Command{
			Name: "admin_cancel", Event: "dance", Target: "alice", TargetVersion: 1,
		}},
		Outcome: agent.ScriptToolResult{Name: name},
	}
}

func TestAgentHostLedgerConcurrentAdmissionKeepsFinalWitness(t *testing.T) {
	t.Parallel()
	testLedgerConflict(t, false)
}

func TestAgentHostLedgerConflictExhaustionDoesNotAdmitIntent(t *testing.T) {
	t.Parallel()
	testLedgerConflict(t, true)
}

func testLedgerConflict(t *testing.T, exhaust bool) {
	t.Helper()
	f := passMenuFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const owner = "bob"
	const updateID int64 = 44992
	base := agenthost.ScriptStore{DB: f.db, Policy: liveLedgerAuthority{fixture: f}, StaleError: appclient.ErrReadStale}
	generation, err := f.b.API.HistoryGeneration(ctx, owner)
	require.NoError(t, err)
	index, err := base.ReserveRun(
		ctx,
		owner,
		updateID,
		agent.ScriptProposal{Code: "return null;", InputJSON: "null"},
		generation,
		nil,
		nil,
		false,
	)
	require.NoError(t, err)
	competing := 0
	policy := liveLedgerAuthority{
		fixture: f,
		after: func(checkContext context.Context, record agenthost.ScriptRecord) error {
			if len(record.Calls) == 0 || record.Calls[len(record.Calls)-1].Outcome.Name != "outer" ||
				!exhaust && competing > 0 {
				return nil
			}
			// This admission uses the same ledger and real authority APIs. It must
			// finish while the outer authorization is still on the stack.
			call := ledgerAdmission(fmt.Sprintf("competing-%d", competing))
			_, admissionErr := base.AdmitCall(checkContext, owner, updateID, index, &call)
			if admissionErr == nil {
				competing++
			}
			return admissionErr
		},
	}
	outer := base
	outer.Policy = policy
	call := ledgerAdmission("outer")
	sequence, err := outer.AdmitCall(ctx, owner, updateID, index, &call)
	if exhaust {
		require.ErrorIs(t, err, agenthost.ErrScriptLedgerConflict)
		require.NotErrorIs(t, err, appclient.ErrReadStale)
		require.NoError(t, ctx.Err(), "contention must not become cancellation or a lock timeout")
		require.Empty(t, call.Pass.Command.Key, "uncommitted tentative keys must not escape")
		require.Nil(t, call.Pass.Witness)
		require.Equal(t, 4, competing, "the retry bound must be finite")
	} else {
		require.NoError(t, err)
		require.Equal(t, 1, competing)
		require.Equal(t, 1, sequence)
		require.Equal(t, agenthost.ScriptToolKey(updateID, index, sequence), call.Pass.Command.Key)
		witness, witnessErr := passbooking.CommandOperationWitness(owner, *call.Pass.Command)
		require.NoError(t, witnessErr)
		require.NotNil(t, call.Pass.Witness)
		require.Equal(t, witness, *call.Pass.Witness)
	}
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`, owner, updateID).
			Scan(&records),
	)
	require.Len(t, records, 1)
	want := competing
	if !exhaust {
		want++
	}
	require.Len(t, records[0].Calls, want)
	require.Equal(t, "interrupted", records[0].Run.Error, "ledger contention must not finalize the worker run")
	require.False(t, records[0].PassRedacted)
	for position, saved := range records[0].Calls {
		require.Equal(t, agenthost.ScriptToolKey(updateID, index, position), saved.Pass.Command.Key)
		require.NotNil(t, saved.Pass.Witness)
		witness, witnessErr := passbooking.CommandOperationWitness(owner, *saved.Pass.Command)
		require.NoError(t, witnessErr)
		require.Equal(t, witness, *saved.Pass.Witness)
	}
}

type pausedRetirementAuthority struct {
	liveLedgerAuthority

	observed chan struct{}
	release  chan struct{}
}

func (policy pausedRetirementAuthority) AccessChanged(
	ctx context.Context,
	owner string,
	record agenthost.ScriptRecord,
) (bool, error) {
	if _, err := policy.liveLedgerAuthority.AccessChanged(ctx, owner, record); err != nil {
		return false, err
	}
	if record.Request.Code != "return 'old';" {
		return false, nil
	}
	close(policy.observed)
	select {
	case <-policy.release:
		return true, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

func TestAgentHostRetirementRepairPreservesNewAdmission(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			const owner = "alice"
			const updateID int64 = 44993
			basePolicy := liveLedgerAuthority{fixture: f}
			store := agenthost.ScriptStore{DB: f.db, Policy: basePolicy, StaleError: appclient.ErrReadStale}
			generation, err := basePolicy.Generation(ctx, owner)
			require.NoError(t, err)
			_, err = store.ReserveRun(
				ctx,
				owner,
				updateID,
				agent.ScriptProposal{Code: "return 'old';", InputJSON: "null"},
				generation,
				nil,
				nil,
				false,
			)
			require.NoError(t, err)
			policy := pausedRetirementAuthority{
				liveLedgerAuthority: basePolicy,
				observed:            make(chan struct{}),
				release:             make(chan struct{}),
			}
			retiring := store
			retiring.Policy = policy
			originalCtx, stopOriginal := context.WithCancel(ctx)
			defer stopOriginal()
			done := make(chan error, 1)
			go func() {
				_, loadErr := retiring.LoadAuthorized(originalCtx, owner, updateID)
				done <- loadErr
			}()
			select {
			case <-policy.observed:
			case <-ctx.Done():
				t.Fatal("retirement observation did not reach its barrier")
			}
			index, err := store.ReserveRun(
				ctx,
				owner,
				updateID,
				agent.ScriptProposal{Code: "return 'fresh';", InputJSON: "null"},
				generation,
				nil,
				nil,
				false,
			)
			require.NoError(t, err)
			require.Equal(t, 1, index)
			_, err = store.CompleteRun(
				ctx,
				owner,
				updateID,
				0,
				agent.ScriptRun{Result: json.RawMessage(`"concurrent-private-progress"`)},
			)
			require.NoError(t, err)
			if canceled {
				stopOriginal()
			} else {
				close(policy.release)
			}
			select {
			case err = <-done:
				if canceled {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.NoError(t, err)
				}
			case <-ctx.Done():
				t.Fatal("retirement repair did not finish")
			}
			var records []agenthost.ScriptRecord
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`, owner, updateID).
					Scan(&records),
			)
			require.Len(t, records, 2)
			require.True(t, records[0].PassRedacted)
			require.NotContains(
				t,
				string(records[0].Run.Result),
				"concurrent-private-progress",
				"ordinary progress must not evade the original retirement",
			)
			require.False(
				t,
				records[1].PassRedacted,
				"repair may not retire a later independently authorized admission",
			)
			require.Equal(t, "return 'fresh';", records[1].Request.Code)
			require.Equal(t, "interrupted", records[1].Run.Error)
		})
	}
}
