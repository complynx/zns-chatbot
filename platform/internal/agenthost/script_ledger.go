package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const scriptLedgerAttempts = 4

// ErrScriptLedgerConflict asks the caller to retry durable recovery. It is not
// evidence of source retirement and must never finalize a worker run.
var ErrScriptLedgerConflict = errors.New("script ledger changed concurrently")

type scriptLedgerSnapshot struct {
	raw     json.RawMessage
	records []ScriptRecord
}

func (s ScriptStore) ledgerSnapshot(ctx context.Context, owner string, updateID int64) (scriptLedgerSnapshot, error) {
	var snapshot scriptLedgerSnapshot
	err := s.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, scriptRunsKind).
		Scan(&snapshot.raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, core.DatabaseOperationContextError(ctx, err)
	}
	return snapshot, json.Unmarshal(snapshot.raw, &snapshot.records)
}

// Only the SQL-only choice claim runs after the revision check. Authorization,
// preparation and result encoding finish before this transaction starts.
func (s ScriptStore) commitLedger(ctx context.Context, owner string, updateID int64,
	snapshot scriptLedgerSnapshot, write bool, claim func(pgx.Tx, []ScriptRecord) error) (bool, error) {
	if !write && claim == nil {
		// Authorization already inspected this revision. A read-only revision
		// check needs no row lock or durable write transaction.
		var current json.RawMessage
		err := s.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, scriptRunsKind).
			Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, core.DatabaseOperationContextError(ctx, err)
		}
		return bytes.Equal(current, snapshot.raw), nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = LockScript(ctx, tx, owner, updateID); err != nil {
		return false, err
	}
	var current json.RawMessage
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, scriptRunsKind).
		Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, core.DatabaseOperationContextError(ctx, err)
	}
	if !bytes.Equal(current, snapshot.raw) {
		return false, nil
	}
	if claim != nil {
		if err = claim(tx, snapshot.records); err != nil {
			return false, err
		}
	}
	if write {
		_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, scriptRunsKind, snapshot.records)
		if err != nil {
			return false, core.DatabaseOperationContextError(ctx, err)
		}
	}
	return true, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}

func scriptRetired(record ScriptRecord) bool {
	return record.PassRedacted || record.HistoryRedacted || record.MemoryRedacted
}

func scriptRunIdentity(record ScriptRecord) ([]byte, error) {
	// Calls may progress independently, but the original admitted run cannot be
	// replaced or rebound while one of its completions is waiting to commit.
	record.Calls = nil
	record.Run.Calls = nil
	record.Run.Result = nil
	return json.Marshal(record)
}

func scriptCallIdentity(call ScriptToolRecord) ([]byte, error) {
	call.KnowledgeRefreshPending = false
	call.Outcome.Result = nil
	call.Outcome.Error = ""
	call.ResultAuthorities = nil
	return json.Marshal(call)
}

func cloneScriptCall(call ScriptToolRecord) (ScriptToolRecord, error) {
	raw, err := json.Marshal(call)
	if err != nil {
		return ScriptToolRecord{}, err
	}
	var detached ScriptToolRecord
	if err = json.Unmarshal(raw, &detached); err != nil {
		return detached, err
	}
	if call.Profile != nil {
		// Profile values deliberately never cross the JSON ledger boundary.
		detached.Profile.Value = call.Profile.Value
	}
	return detached, nil
}

type scriptRetirementTarget struct {
	index    int
	identity []byte
}

func scriptAdmissionIdentity(record ScriptRecord) ([]byte, error) {
	return json.Marshal(ScriptRecord{PrivateHistory: record.PrivateHistory,
		HistoryGeneration: record.HistoryGeneration, MemoryState: record.MemoryState,
		ReadAuthorities: record.ReadAuthorities, PassContext: record.PassContext})
}

func (s ScriptStore) retirementError(
	ctx context.Context,
	owner string,
	updateID int64,
	cause error,
	raw json.RawMessage,
) error {
	var observed []ScriptRecord
	if err := json.Unmarshal(raw, &observed); err != nil {
		return errors.Join(cause, err)
	}
	targets := make([]scriptRetirementTarget, 0, len(observed))
	for index, record := range observed {
		identity, err := scriptAdmissionIdentity(record)
		if err != nil {
			return errors.Join(cause, err)
		}
		targets = append(targets, scriptRetirementTarget{index: index, identity: identity})
	}
	repairCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), scriptCallTimeout)
	defer cancel()
	return errors.Join(cause, s.persistRetirement(repairCtx, owner, updateID, targets))
}

func (s ScriptStore) authorizeLedger(
	ctx context.Context,
	owner string,
	updateID int64,
	snapshot scriptLedgerSnapshot,
) error {
	retired, err := s.reauthorize(ctx, owner, snapshot.records)
	if retired {
		return s.retirementError(ctx, owner, updateID, errors.Join(s.StaleError, err), snapshot.raw)
	}
	return err
}
