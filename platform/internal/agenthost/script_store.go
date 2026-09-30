package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const scriptRunsKind = "script_runs"
const MaxScriptCalls = 8
const scriptInterrupted = "interrupted"

// ScriptAuthority supplies current domain authorization outside ledger locks.
type ScriptAuthority interface {
	Generation(context.Context, string) (int64, error)
	MemoryState(context.Context, string) (knowledge.MemoryDeletionState, error)
	AccessChanged(context.Context, string, ScriptRecord) (bool, error)
}

// ScriptStore is the sole script reservation writer. Its SQL and lock identity
// preserve interrupted runs and calls across process restarts.
type ScriptStore struct {
	Reads      ReadStore
	DB         *pgxpool.Pool
	Policy     ScriptAuthority
	StaleError error
}

func ScriptToolKey(updateID int64, index, sequence int) string {
	return fmt.Sprintf("tg-script-%d-%d-%d", updateID, index, sequence)
}
func (s ScriptStore) Records(ctx context.Context, owner string, updateID int64) ([]ScriptRecord, error) {
	generation, generationErr := s.Policy.Generation(ctx, owner)
	if generationErr != nil {
		return nil, generationErr
	}
	if generationErr = s.Reads.ReconcileHistory(ctx, owner, generation); generationErr != nil {
		return nil, generationErr
	}
	state, stateErr := s.Policy.MemoryState(ctx, owner)
	if stateErr != nil {
		return nil, stateErr
	}
	if stateErr = s.Reads.ReconcileMemory(ctx, owner, state); stateErr != nil {
		return nil, stateErr
	}
	records, err := s.LoadAuthorized(ctx, owner, updateID)
	for index := range records {
		RedactProfileScript(&records[index])
		RedactDeletedScript(&records[index], state)
		RedactHistoryScript(&records[index], generation)
	}
	return records, err
}

func LockScript(ctx context.Context, tx pgx.Tx, owner string, updateID int64) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"script:"+owner+":"+strconv.FormatInt(updateID, 10),
	)
	return core.DatabaseOperationContextError(ctx, err)
}

func (s ScriptStore) CompleteRecord(
	record *ScriptRecord,
	run agent.ScriptRun,
	state knowledge.MemoryDeletionState,
	generation int64,
) {
	record.Run = run
	// Terminal requests are never re-executed. Retain effect identities and
	// evidence, without keeping extra copies of arbitrary source/input text.
	record.Request = agent.ScriptProposal{}
	record.Run.Code = ""
	for callIndex := range record.Calls {
		if record.Calls[callIndex].Memory != nil {
			record.Calls[callIndex].Memory.Text = ""
		}
	}
	RedactProfileScript(record)
	RedactDeletedScript(record, state)
	RedactHistoryScript(record, generation)
}

func (s ScriptStore) reauthorize(ctx context.Context, owner string, records []ScriptRecord) (bool, error) {
	retired := false
	var failure error
	for _, record := range records {
		if scriptRetired(record) {
			continue
		}
		changed, err := s.Policy.AccessChanged(ctx, owner, record)
		retired = retired || changed
		if err != nil {
			failure = err
			break
		}
	}
	if retired {
		for index := range records {
			RedactPassScript(&records[index])
		}
	}
	return retired, failure
}

// Repair only admissions examined before the detached authority check. The
// latest row preserves concurrent progress and independently appended runs.
func (s ScriptStore) persistRetirement(
	ctx context.Context,
	owner string,
	updateID int64,
	targets []scriptRetirementTarget,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = LockScript(ctx, tx, owner, updateID); err != nil {
		return err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, scriptRunsKind).
		Scan(&raw); err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	var records []ScriptRecord
	if raw != nil {
		if err = json.Unmarshal(raw, &records); err != nil {
			return err
		}
	}
	for _, target := range targets {
		if target.index >= len(records) {
			continue
		}
		identity, identityErr := scriptAdmissionIdentity(records[target.index])
		if identityErr != nil {
			return identityErr
		}
		if bytes.Equal(identity, target.identity) || scriptRetired(records[target.index]) {
			RedactPassScript(&records[target.index])
		}
	}
	// Encode JSON null explicitly; a nil slice parameter would become SQL NULL.
	raw, err = json.Marshal(records)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		updateID,
		scriptRunsKind,
		raw,
	); err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	return core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}

func (s ScriptStore) AdmitSource(ctx context.Context, owner string, updateID int64, index int) error {
	records, err := s.LoadAuthorized(ctx, owner, updateID)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(records) {
		return errors.New("script reservation missing")
	}
	current := records[index]
	if current.PassRedacted || current.HistoryRedacted || current.MemoryRedacted {
		return s.StaleError
	}
	return nil
}

func (s ScriptStore) ReserveRun(ctx context.Context, owner string, updateID int64,
	p agent.ScriptProposal, generation int64, registration *agent.RegistrationContext,
	authorities []readsource.Authority, privateHistory bool) (int, error) {
	for range scriptLedgerAttempts {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		snapshot, err := s.ledgerSnapshot(ctx, owner, updateID)
		if err != nil {
			return 0, err
		}
		if len(snapshot.records) >= agent.MaxScriptRuns {
			return 0, errors.New("script budget exhausted")
		}
		current, err := s.Policy.Generation(ctx, owner)
		if err != nil {
			return 0, err
		}
		if current != generation {
			return 0, s.StaleError
		}
		state, err := s.Policy.MemoryState(ctx, owner)
		if err != nil {
			return 0, err
		}
		record := ScriptRecord{
			PrivateHistory:    privateHistory,
			ReadAuthorities:   readsource.CloneAuthorities(authorities),
			PassContext:       ScriptPassContext(registration),
			HistoryGeneration: generation,
			MemoryState:       state,
			Request:           p,
			Run:               agent.ScriptRun{Code: p.Code, Error: scriptInterrupted},
		}
		if err = s.authorizeNewRecord(ctx, owner, record); err != nil {
			return 0, err
		}
		index := len(snapshot.records)
		snapshot.records = append(snapshot.records, record)
		committed, err := s.commitLedger(ctx, owner, updateID, snapshot, true, nil)
		if err != nil {
			return 0, err
		}
		if committed {
			return index, nil
		}
	}
	return 0, ErrScriptLedgerConflict
}

// updateLedger retries only a detached ledger edit. Domain execution and worker
// evaluation are outside this function and are never repeated on a conflict.
func (s ScriptStore) updateLedger(ctx context.Context, owner string, updateID int64, index int,
	change func([]ScriptRecord, int64) error, claim func(pgx.Tx, []ScriptRecord) error) error {
	var identity []byte
	for range scriptLedgerAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, err := s.ledgerSnapshot(ctx, owner, updateID)
		if err != nil {
			return err
		}
		identity, err = s.matchRunIdentity(snapshot.records, index, identity)
		if err != nil {
			return err
		}
		if err = s.prepareLedgerChange(ctx, owner, updateID, index, snapshot, change); err != nil {
			return err
		}
		committed, err := s.commitLedger(ctx, owner, updateID, snapshot, true, claim)
		if err != nil {
			return err
		}
		if committed {
			return nil
		}
	}
	return ErrScriptLedgerConflict
}

func (s ScriptStore) CompleteRun(ctx context.Context, owner string, updateID int64, index int,
	run agent.ScriptRun) ([]ScriptRecord, error) {
	state, err := s.Policy.MemoryState(ctx, owner)
	if err != nil {
		return nil, err
	}
	var result []ScriptRecord
	err = s.updateLedger(ctx, owner, updateID, index, func(records []ScriptRecord, generation int64) error {
		s.CompleteRecord(&records[index], run, state, generation)
		result = records
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s ScriptStore) AdmitCall(ctx context.Context, owner string, updateID int64, index int,
	call *ScriptToolRecord) (int, error) {
	intent, err := cloneScriptCall(*call)
	if err != nil {
		return 0, err
	}
	var captured *readsource.Derivation
	var admitted ScriptToolRecord
	sequence := 0
	err = s.updateLedger(ctx, owner, updateID, index, func(records []ScriptRecord, _ int64) error {
		record := &records[index]
		if len(record.Calls) >= MaxScriptCalls || record.Run.Error != scriptInterrupted {
			return errors.New("script call budget exhausted")
		}
		candidate, cloneErr := cloneScriptCall(intent)
		if cloneErr != nil {
			return cloneErr
		}
		sequence = len(record.Calls)
		if captured == nil {
			captured, cloneErr = admittedScriptSource(owner, records, index, candidate.Source)
			if cloneErr != nil {
				return cloneErr
			}
		}
		source := captured.Clone()
		candidate.Source = &source
		if !source.Valid() {
			return errors.New("invalid admitted source")
		}
		if _, captureErr := readsource.Capture(owner, source); captureErr != nil {
			return captureErr
		}
		// The candidate key is private until its exact revision commits.
		BindScriptToolKey(&candidate, ScriptToolKey(updateID, index, sequence), index*MaxScriptCalls+sequence+1)
		if witnessErr := captureRegistrationWitness(owner, &candidate); witnessErr != nil {
			return witnessErr
		}
		record.Calls = append(record.Calls, candidate)
		return nil
	}, func(tx pgx.Tx, records []ScriptRecord) error {
		candidate := &records[index].Calls[sequence]
		if choiceErr := s.admitModernChoice(
			ctx,
			tx,
			owner,
			updateID,
			index,
			sequence,
			records,
			candidate,
		); choiceErr != nil {
			return choiceErr
		}
		admitted = *candidate
		return nil
	})
	if err == nil {
		restoreAdmittedProfile(&admitted, intent)
		*call = admitted
	}
	return sequence, err
}

// Restore only the live value after admission commits; ledger copies stay redacted.
func restoreAdmittedProfile(admitted *ScriptToolRecord, intent ScriptToolRecord) {
	if admitted.Profile != nil {
		profile := *admitted.Profile
		profile.Value = intent.Profile.Value
		admitted.Profile = &profile
	}
}

func (s ScriptStore) CompleteCall(ctx context.Context, owner string, updateID int64,
	index, sequence int, call ScriptToolRecord) error {
	if handled, err := s.completeRegistrationCall(ctx, owner, updateID, index, sequence, call); handled {
		return err
	}
	identity, err := scriptCallIdentity(call)
	if err != nil {
		return err
	}
	return s.updateLedger(ctx, owner, updateID, index, func(records []ScriptRecord, _ int64) error {
		record := &records[index]
		if sequence < 0 || sequence >= len(record.Calls) {
			return errors.New("script call reservation missing")
		}
		current, identityErr := scriptCallIdentity(record.Calls[sequence])
		if identityErr != nil {
			return identityErr
		}
		if !bytes.Equal(identity, current) {
			return ErrScriptLedgerConflict
		}
		detached, cloneErr := cloneScriptCall(call)
		if cloneErr != nil {
			return cloneErr
		}
		record.Calls[sequence] = detached
		return nil
	}, nil)
}

func (s ScriptStore) LoadAuthorized(ctx context.Context, owner string, updateID int64) ([]ScriptRecord, error) {
	for range scriptLedgerAttempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		snapshot, err := s.ledgerSnapshot(ctx, owner, updateID)
		if err != nil {
			return nil, err
		}
		if len(snapshot.raw) == 0 {
			return []ScriptRecord{}, nil
		}
		repairedExisting := normalizeRetiredScripts(snapshot.records)
		retired, authorizationErr := s.reauthorize(ctx, owner, snapshot.records)
		if retired {
			if err = s.retirementError(ctx, owner, updateID, authorizationErr, snapshot.raw); err != nil {
				return nil, err
			}
			continue
		}
		if authorizationErr != nil {
			return nil, authorizationErr
		}
		unchanged, err := s.commitLedger(ctx, owner, updateID, snapshot, repairedExisting, nil)
		if err != nil {
			return nil, err
		}
		if unchanged {
			return snapshot.records, nil
		}
	}
	return nil, ErrScriptLedgerConflict
}

func (s ScriptStore) MarkPrivateProfile(ctx context.Context, owner string, id int64, index int) error {
	return s.updateLedger(ctx, owner, id, index, func(records []ScriptRecord, _ int64) error {
		record := &records[index]
		record.PrivateProfile = true
		record.Request.Code, record.Request.InputJSON, record.Run.Code = "", "", ""
		return nil
	}, nil)
}

func (s ScriptStore) authorizeNewRecord(ctx context.Context, owner string, record ScriptRecord) error {
	changed, err := s.Policy.AccessChanged(ctx, owner, record)
	if err != nil {
		return err
	}
	if changed {
		return s.StaleError
	}
	return nil
}

func (s ScriptStore) matchRunIdentity(records []ScriptRecord, index int, identity []byte) ([]byte, error) {
	if index < 0 || index >= len(records) {
		return nil, errors.New("script reservation missing")
	}
	if scriptRetired(records[index]) {
		return nil, s.StaleError
	}
	current, err := scriptRunIdentity(records[index])
	if err != nil {
		return nil, err
	}
	if identity != nil && !bytes.Equal(identity, current) {
		return nil, ErrScriptLedgerConflict
	}
	return current, nil
}

func (s ScriptStore) prepareLedgerChange(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	snapshot scriptLedgerSnapshot,
	change func([]ScriptRecord, int64) error,
) error {
	generation, err := s.Policy.Generation(ctx, owner)
	if err != nil {
		return err
	}
	if generation != snapshot.records[index].HistoryGeneration {
		return s.retirementError(ctx, owner, updateID, s.StaleError, snapshot.raw)
	}
	if err = s.authorizeLedger(ctx, owner, updateID, snapshot); err != nil {
		return err
	}
	if err = change(snapshot.records, generation); err != nil {
		return err
	}
	if err = s.authorizeLedger(ctx, owner, updateID, snapshot); err != nil {
		return err
	}
	if scriptRetired(snapshot.records[index]) {
		return s.retirementError(ctx, owner, updateID, s.StaleError, snapshot.raw)
	}
	RedactProfileScript(&snapshot.records[index])
	return nil
}

func admittedScriptSource(
	owner string,
	records []ScriptRecord,
	index int,
	original *readsource.Derivation,
) (*readsource.Derivation, error) {
	if original != nil {
		source := original.Clone()
		return &source, nil
	}
	refs, err := ScriptReadAuthorities(owner, records)
	if err != nil {
		return nil, err
	}
	generation := records[index].HistoryGeneration
	source := readsource.Derivation{Generation: &generation, Authorities: refs}
	for _, run := range records {
		source.PrivateHistory = source.PrivateHistory || ScriptPrivateHistory(run)
	}
	return &source, nil
}

func normalizeRetiredScripts(records []ScriptRecord) bool {
	repairedExisting := false
	for index := range records {
		record := &records[index]
		switch {
		case record.PassRedacted:
			RedactPassScript(record)
		case record.HistoryRedacted:
			RedactHistoryScript(record, record.HistoryGeneration)
		case record.MemoryRedacted:
			RedactDeletedScript(record, record.MemoryState)
		default:
			continue
		}
		repairedExisting = true
	}
	return repairedExisting
}
