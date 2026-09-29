package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const knowledgeReadsKind = "knowledge_reads"
const registrationReadsKind = "registration_reads"
const historyReadInterrupted = "interrupted"

type ReadAuthority interface {
	Generation(context.Context, string) (int64, error)
	Registration(context.Context, string, *agent.RegistrationReadResult) error
}

// ReadStore owns consumed read budgets and privacy reconciliation. Domain
// fetches run outside the transactions, after durable reservation.
type ReadStore struct {
	DB     *pgxpool.Pool
	Policy ReadAuthority
}

func (s ReadStore) ReserveHistory(ctx context.Context, owner string, updateID int64) (int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = historyReadLock(ctx, tx, owner, updateID); err != nil {
		return 0, err
	}
	var reads []conversation.Page
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`, owner, updateID).
		Scan(&reads)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if len(reads) >= agent.MaxHistoryReads {
		return 0, errors.New("history read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, conversation.Page{Events: []conversation.Event{}, Error: historyReadInterrupted})
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,'history_reads',$3)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, reads)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func (s ReadStore) CompleteHistory(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	page conversation.Page,
) ([]conversation.Page, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = historyReadLock(ctx, tx, owner, updateID); err != nil {
		return nil, err
	}
	var reads []conversation.Page
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`, owner, updateID).
		Scan(&reads); err != nil {
		return nil, err
	}
	if index >= len(reads) {
		return nil, errors.New("history reservation missing")
	}
	generation, generationErr := s.Policy.Generation(ctx, owner)
	if generationErr != nil {
		return nil, generationErr
	}
	reads[index] = page
	RedactHistoryPages(reads, generation)
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$3 WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`,
		owner,
		updateID,
		reads,
	)
	if err != nil {
		return nil, err
	}
	return reads, tx.Commit(ctx)
}

func (s ReadStore) History(ctx context.Context, owner string, updateID int64) ([]conversation.Page, error) {
	var reads []conversation.Page
	err := s.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`, owner, updateID).
		Scan(&reads)
	if errors.Is(err, pgx.ErrNoRows) {
		return []conversation.Page{}, nil
	}
	if err != nil {
		return nil, err
	}
	generation, err := s.Policy.Generation(ctx, owner)
	if err != nil {
		return nil, err
	}
	if err = s.ReconcileHistory(ctx, owner, generation); err != nil {
		return nil, err
	}
	RedactHistoryPages(reads, generation)
	return reads, nil
}

func historyReadLock(ctx context.Context, tx pgx.Tx, owner string, updateID int64) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"history-read:"+owner+":"+strconv.FormatInt(updateID, 10),
	)
	return err
}

func (s ReadStore) Knowledge(ctx context.Context, owner string, updateID int64) ([]agent.KnowledgeReadResult, error) {
	var reads []agent.KnowledgeReadResult
	err := s.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, knowledgeReadsKind).
		Scan(&reads)
	if errors.Is(err, pgx.ErrNoRows) {
		return []agent.KnowledgeReadResult{}, nil
	}
	return reads, err
}

func (s ReadStore) ReserveKnowledge(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.KnowledgeProposal,
) (int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := knowledge.LockMemoryDeletions(ctx, tx, owner)
	if err != nil {
		return 0, err
	}
	if err = knowledgeReadLock(ctx, tx, owner, updateID); err != nil {
		return 0, err
	}
	var reads []agent.KnowledgeReadResult
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, knowledgeReadsKind).
		Scan(&reads)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	RedactKnowledgeReads(reads, state)
	if len(reads) >= agent.MaxKnowledgeReads {
		return 0, errors.New("knowledge read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, agent.KnowledgeReadResult{Request: p, MemoryState: state, Error: "interrupted"})
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, knowledgeReadsKind, reads)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func knowledgeReadLock(ctx context.Context, tx pgx.Tx, owner string, updateID int64) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"knowledge-read:"+owner+":"+strconv.FormatInt(updateID, 10),
	)
	return err
}

func (s ReadStore) ReserveRegistration(
	ctx context.Context,
	owner string,
	id int64,
	p agent.RegistrationProposal,
) (int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		registrationReadsKind+":"+owner+":"+strconv.FormatInt(id, 10),
	); err != nil {
		return 0, err
	}
	reads := []agent.RegistrationReadResult{}
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, registrationReadsKind).
		Scan(&reads)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	for _, read := range reads {
		if read.Request == p {
			return 0, errors.New("registration read already available")
		}
	}
	if len(reads) >= agent.MaxRegistrationReads {
		return 0, errors.New("registration read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, agent.RegistrationReadResult{Request: p, Error: "read_pending"})
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
	ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, id, registrationReadsKind, reads)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func (s ReadStore) Registration(
	ctx context.Context,
	owner string,
	id int64,
) ([]agent.RegistrationReadResult, error) {
	reads := []agent.RegistrationReadResult{}
	err := s.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, registrationReadsKind).
		Scan(&reads)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	for index := range reads {
		if err = s.Policy.Registration(ctx, owner, &reads[index]); err != nil {
			return nil, err
		}
	}
	return reads, nil
}

func (s ReadStore) ReconcileHistory(ctx context.Context, owner string, generation int64) error {
	if generation == 0 {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(
		ctx,
		`SELECT update_id,kind,content FROM bot.interactions WHERE owner=$1 AND kind IN ('history_reads','script_runs')
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(content) r WHERE COALESCE((CASE WHEN kind='script_runs' THEN r->>'history_generation' ELSE r->>'generation' END)::bigint,0)<>$2)
 ORDER BY update_id,kind LIMIT 100 FOR UPDATE SKIP LOCKED`,
		owner,
		generation,
	)
	if err != nil {
		return err
	}
	type retained struct {
		updateID int64
		kind     string
		content  json.RawMessage
	}
	var batch []retained
	for rows.Next() {
		var item retained
		if err = rows.Scan(&item.updateID, &item.kind, &item.content); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range batch {
		content, redactErr := RedactHistoryInteraction(item.kind, item.content, generation)
		if redactErr != nil {
			return redactErr
		}
		if _, err = tx.Exec(
			ctx,
			`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
			owner,
			item.updateID,
			item.kind,
			content,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s ReadStore) ReconcileMemory(ctx context.Context, owner string, _ knowledge.MemoryDeletionState) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A caller's captured epochs can be older than an already committed read.
	// Acquire current domain generations before taking interaction row locks.
	state, err := knowledge.LockMemoryDeletions(ctx, tx, owner)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT update_id,kind,content FROM bot.interactions WHERE owner=$1 AND kind IN ($2,$4)
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(content) r WHERE COALESCE(r->'memory_state','{}'::jsonb)<>$3::jsonb)
 ORDER BY update_id,kind LIMIT 100 FOR UPDATE SKIP LOCKED`, owner, scriptRunsKind, state, knowledgeReadsKind)
	if err != nil {
		return err
	}
	type retained struct {
		updateID int64
		kind     string
		content  json.RawMessage
	}
	var batch []retained
	for rows.Next() {
		var item retained
		if err = rows.Scan(&item.updateID, &item.kind, &item.content); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range batch {
		content, redactErr := RedactMemoryInteraction(item.kind, item.content, state)
		if redactErr != nil {
			return redactErr
		}
		if _, err = tx.Exec(
			ctx,
			`UPDATE bot.interactions SET content=$3 WHERE owner=$1 AND update_id=$2 AND kind=$4`,
			owner,
			item.updateID,
			content,
			item.kind,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s ReadStore) CompleteKnowledge(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	result agent.KnowledgeReadResult,
) ([]agent.KnowledgeReadResult, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := knowledge.LockMemoryDeletions(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if err = knowledgeReadLock(ctx, tx, owner, updateID); err != nil {
		return nil, err
	}
	var reads []agent.KnowledgeReadResult
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, knowledgeReadsKind).
		Scan(&reads); err != nil {
		return nil, err
	}
	if index < 0 || index >= len(reads) {
		return nil, errors.New("knowledge read reservation missing")
	}
	// Retirement belongs to the reservation, not the delayed fetch's epoch.
	RedactKnowledgeReads(reads, state)
	if !reads[index].Omitted {
		result.Request = reads[index].Request
		reads[index] = result
	}
	// Reconcile the locked current row, including siblings, against the
	// domain generations retained through commit. A late fetch cannot revive it.
	RedactKnowledgeReads(reads, state)
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		updateID,
		knowledgeReadsKind,
		reads,
	)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return reads, nil
}

func (s ReadStore) CompleteRegistration(
	ctx context.Context,
	owner string,
	id int64,
	index int,
	result agent.RegistrationReadResult,
) error {
	_, err := s.DB.Exec(ctx, `UPDATE bot.interactions SET content=jsonb_set(content,ARRAY[$4],$5::jsonb)
 WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, registrationReadsKind, strconv.Itoa(index), result)
	return err
}

// ReserveHistorySummary consumes the attempt before reading or calling a model.
func (s ReadStore) ReserveHistorySummary(ctx context.Context, owner string, updateID int64) error {
	var claimed bool
	return s.DB.QueryRow(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES($1,$2,'history_summary_attempt','{}') ON CONFLICT DO NOTHING RETURNING true`, owner, updateID).Scan(&claimed)
}
