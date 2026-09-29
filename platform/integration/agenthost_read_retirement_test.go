package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type completionBarrierKey struct{}
type completionQueryKey struct{}

// A reconciliation that only acquires its gate after completion must not repair
// the completion's committed row before the test observes that row.
type reconciliationReadBarrier struct {
	completionReleased <-chan struct{}
	repairAllowed      <-chan struct{}
}

func (b reconciliationReadBarrier) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT update_id,kind,content FROM bot.interactions") {
		select {
		case <-b.completionReleased:
			select {
			case <-b.repairAllowed:
			case <-ctx.Done():
			}
		default:
		}
	}
	return ctx
}

func (reconciliationReadBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Pause the actual completion SELECT after PostgreSQL has produced its row.
// The production query decides whether the transaction retains a row lock.
type knowledgeCompletionBarrier struct {
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *knowledgeCompletionBarrier) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	selected, _ := ctx.Value(completionBarrierKey{}).(bool)
	if selected && strings.HasPrefix(data.SQL, "SELECT content FROM bot.interactions") {
		return context.WithValue(ctx, completionQueryKey{}, true)
	}
	return ctx
}

func (b *knowledgeCompletionBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	selected, _ := ctx.Value(completionQueryKey{}).(bool)
	if !selected || data.Err != nil {
		return
	}
	b.once.Do(func() {
		close(b.reached)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
}

// The C4c baseline delegates its scrub transformation to a leaf. This fixture
// supplies the expected scrubbed row; C4d performs the transformation itself.
type retiredKnowledgeRow struct {
	agenthost.ReadAuthority

	reads []agent.KnowledgeReadResult
}

func (r retiredKnowledgeRow) MemoryInteraction(
	string,
	json.RawMessage,
	knowledge.MemoryDeletionState,
) (json.RawMessage, error) {
	return json.Marshal(r.reads)
}

func TestAgentHostKnowledgeCompletionCannotRestoreRetiredReads(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	const owner = "alice"
	const updateID int64 = 94002
	const secret = "retired-private-memory-body"
	_, err := s.Execute(
		ctx,
		owner,
		knowledge.Command{Name: knowledge.MemoSet, Key: "read-race-create", FactKey: "read-race", Text: secret},
	)
	require.NoError(t, err)

	store := agenthost.ReadStore{DB: s.DB}
	first := agent.KnowledgeProposal{Text: "retired first request"}
	second := agent.KnowledgeProposal{Text: "retired second request"}
	_, err = store.ReserveKnowledge(ctx, owner, updateID, first)
	require.NoError(t, err)
	_, err = store.ReserveKnowledge(ctx, owner, updateID, second)
	require.NoError(t, err)
	fetched := agent.KnowledgeReadResult{Request: second, Memo: &knowledge.Memo{Text: secret}}
	_, err = store.CompleteKnowledge(ctx, owner, updateID, 1, fetched)
	require.NoError(t, err)

	_, err = s.Execute(
		ctx,
		owner,
		knowledge.Command{Name: knowledge.MemoDelete, Key: "read-race-delete", FactKey: "read-race", Version: 1},
	)
	require.NoError(t, err)
	state, err := s.MemoryDeletions(ctx, owner)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.PrivateGeneration)

	barrier := &knowledgeCompletionBarrier{reached: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
	defer release()
	config := s.DB.Config()
	config.ConnConfig.Tracer = barrier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer func() { release(); pool.Close() }()
	completion := agenthost.ReadStore{DB: pool}
	fetched.Request = first
	type completionResult struct {
		reads []agent.KnowledgeReadResult
		err   error
	}
	done := make(chan completionResult, 1)
	go func() {
		reads, completeErr := completion.CompleteKnowledge(
			context.WithValue(ctx, completionBarrierKey{}, true),
			owner,
			updateID,
			0,
			fetched,
		)
		done <- completionResult{reads: reads, err: completeErr}
	}()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal("completion did not reach the SELECT barrier")
	}

	scrubbed := []agent.KnowledgeReadResult{
		{MemoryState: state, Error: "memory_deleted", Omitted: true},
		{MemoryState: state, Error: "memory_deleted", Omitted: true},
	}
	store.Policy = retiredKnowledgeRow{reads: scrubbed}
	repairAllowed := make(chan struct{})
	var repairOnce sync.Once
	allowRepair := func() { repairOnce.Do(func() { close(repairAllowed) }) }
	defer allowRepair()
	reconcileConfig := s.DB.Config()
	reconcileConfig.ConnConfig.RuntimeParams["application_name"] = "c4d-reconcile-gate"
	reconcileConfig.ConnConfig.Tracer = reconciliationReadBarrier{
		completionReleased: barrier.release,
		repairAllowed:      repairAllowed,
	}
	reconcilePool, err := pgxpool.NewWithConfig(ctx, reconcileConfig)
	require.NoError(t, err)
	defer func() { release(); allowRepair(); reconcilePool.Close() }()
	store.DB = reconcilePool
	reconciled := make(chan error, 1)
	go func() { reconciled <- store.ReconcileMemory(ctx, owner, state) }()
	reconcileFinished := false
	require.Eventually(t, func() bool {
		select {
		case reconcileErr := <-reconciled:
			require.NoError(t, reconcileErr)
			reconcileFinished = true
			return true
		default:
		}
		var waiting bool
		queryErr := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND application_name='c4d-reconcile-gate' AND wait_event_type='Lock')`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	release()
	var completed completionResult
	select {
	case completed = <-done:
		require.NoError(t, completed.err)
	case <-ctx.Done():
		t.Fatal("completion did not finish after release")
	}
	immediate, err := (agenthost.ReadStore{DB: s.DB}).Knowledge(ctx, owner, updateID)
	require.NoError(t, err)
	for _, reads := range [][]agent.KnowledgeReadResult{completed.reads, immediate} {
		require.Len(t, reads, agent.MaxKnowledgeReads)
		for i, read := range reads {
			assert.True(t, read.Omitted, "completion slot %d must already be retired", i)
			assert.Equal(t, "memory_deleted", read.Error)
			assert.Equal(t, state, read.MemoryState)
		}
		encoded, encodeErr := json.Marshal(reads)
		require.NoError(t, encodeErr)
		assert.NotContains(t, string(encoded), secret)
		assert.NotContains(t, string(encoded), first.Text)
		assert.NotContains(t, string(encoded), second.Text)
	}
	allowRepair()
	if !reconcileFinished {
		select {
		case err = <-reconciled:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("reconciliation did not finish after completion released its domain gate")
		}
	}

	retained, err := store.Knowledge(ctx, owner, updateID)
	require.NoError(t, err)
	require.Len(t, retained, agent.MaxKnowledgeReads)
	for i, read := range retained {
		assert.True(t, read.Omitted, "slot %d must remain retired", i)
		assert.Equal(t, "memory_deleted", read.Error)
		assert.Equal(t, state, read.MemoryState)
	}
	encoded, err := json.Marshal(retained)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secret)
	assert.NotContains(t, string(encoded), first.Text)
	assert.NotContains(t, string(encoded), second.Text)
	_, err = store.ReserveKnowledge(ctx, owner, updateID, first)
	require.ErrorContains(t, err, "budget exhausted")
}

func TestAgentHostKnowledgeCompletionSerializesWithDeletion(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	const owner = "alice"
	const updateID int64 = 94003
	_, err := s.Execute(
		ctx,
		owner,
		knowledge.Command{Name: knowledge.MemoSet, Key: "gate-create", FactKey: "gate", Text: "private gate body"},
	)
	require.NoError(t, err)
	store := agenthost.ReadStore{DB: s.DB}
	_, err = store.ReserveKnowledge(ctx, owner, updateID, agent.KnowledgeProposal{})
	require.NoError(t, err)
	barrier := &knowledgeCompletionBarrier{reached: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
	defer release()
	config := s.DB.Config()
	config.ConnConfig.Tracer = barrier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	defer func() { release(); pool.Close() }()
	done := make(chan error, 1)
	go func() {
		_, completeErr := (agenthost.ReadStore{DB: pool}).CompleteKnowledge(
			context.WithValue(ctx, completionBarrierKey{}, true),
			owner,
			updateID,
			0,
			agent.KnowledgeReadResult{Memo: &knowledge.Memo{Text: "private gate body"}},
		)
		done <- completeErr
	}()
	select {
	case <-barrier.reached:
	case <-ctx.Done():
		t.Fatal("completion did not reach its locked row")
	}

	deletionConfig := s.DB.Config()
	deletionConfig.ConnConfig.RuntimeParams["application_name"] = "c4d-retirement-gate"
	deletionPool, err := pgxpool.NewWithConfig(ctx, deletionConfig)
	require.NoError(t, err)
	defer deletionPool.Close()
	deleted := make(chan error, 1)
	go func() {
		_, deleteErr := (knowledge.Service{DB: deletionPool}).Execute(ctx, owner,
			knowledge.Command{Name: knowledge.MemoDelete, Key: "gate-delete", FactKey: "gate", Version: 1})
		deleted <- deleteErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND application_name='c4d-retirement-gate' AND wait_event_type='Lock')`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	release()
	for _, completion := range []<-chan error{done, deleted} {
		select {
		case err = <-completion:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("completion/deletion lock order did not drain")
		}
	}
	state, err := s.MemoryDeletions(ctx, owner)
	require.NoError(t, err)
	require.NoError(t, store.ReconcileMemory(ctx, owner, state))
	reads, err := store.Knowledge(ctx, owner, updateID)
	require.NoError(t, err)
	require.Len(t, reads, 1)
	assert.True(t, reads[0].Omitted)
	assert.Nil(t, reads[0].Memo)
	assert.Equal(t, state, reads[0].MemoryState)
}

func retireHostReadMemory(t *testing.T, s knowledge.Service, shared bool, key string) {
	t.Helper()
	if shared {
		knowledgeFact(t, s, "", key, "shared body", 0)
		_, err := s.Execute(
			t.Context(),
			"kbadmin",
			knowledge.Command{
				Name:    knowledge.RemoveFact,
				Key:     "remove-" + key,
				Topic:   "travel",
				FactKey: key,
				Version: 1,
			},
		)
		require.NoError(t, err)
		return
	}
	_, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "set-" + key, FactKey: key, Text: "private body"},
	)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoDelete, Key: "remove-" + key, FactKey: key, Version: 1},
	)
	require.NoError(t, err)
}

func TestAgentHostKnowledgeRetiredReservationRejectsNewEpochFetch(t *testing.T) {
	t.Parallel()
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "shared"}[shared], func(t *testing.T) {
			t.Parallel()
			s := knowledgeFixture(t)
			store := agenthost.ReadStore{DB: s.DB}
			request := agent.KnowledgeProposal{Text: "retired request text must stay absent"}
			_, err := store.ReserveKnowledge(t.Context(), "alice", 94101, request)
			require.NoError(t, err)
			retireHostReadMemory(t, s, shared, "reservation")
			state, err := s.MemoryDeletions(t.Context(), "alice")
			require.NoError(t, err)
			require.NoError(t, store.ReconcileMemory(t.Context(), "alice", state))
			incoming := agent.KnowledgeReadResult{
				Request:     request,
				MemoryState: state,
				Memo:        &knowledge.Memo{Text: "new epoch fetched body"},
			}
			returned, err := store.CompleteKnowledge(t.Context(), "alice", 94101, 0, incoming)
			require.NoError(t, err)
			stored, err := store.Knowledge(t.Context(), "alice", 94101)
			require.NoError(t, err)
			for _, reads := range [][]agent.KnowledgeReadResult{returned, stored} {
				require.Len(t, reads, 1)
				assert.True(t, reads[0].Omitted)
				assert.Equal(t, "memory_deleted", reads[0].Error)
				assert.Empty(t, reads[0].Request.Text)
				assert.Nil(t, reads[0].Memo)
				assert.Equal(t, state, reads[0].MemoryState)
			}
		})
	}
}

func TestAgentHostKnowledgeDelayedReconcilePreservesNewEpochRead(t *testing.T) {
	t.Parallel()
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "shared"}[shared], func(t *testing.T) {
			t.Parallel()
			s := knowledgeFixture(t)
			store := agenthost.ReadStore{DB: s.DB}
			retireHostReadMemory(t, s, shared, "first")
			captured, err := s.MemoryDeletions(t.Context(), "alice")
			require.NoError(t, err)
			retireHostReadMemory(t, s, shared, "second")
			current, err := s.MemoryDeletions(t.Context(), "alice")
			require.NoError(t, err)
			require.NotEqual(t, captured, current)
			request := agent.KnowledgeProposal{Text: "current request"}
			_, err = store.ReserveKnowledge(t.Context(), "alice", 94102, request)
			require.NoError(t, err)
			expected := agent.KnowledgeReadResult{
				Request:     request,
				MemoryState: current,
				Memo:        &knowledge.Memo{Text: "current authorized body"},
			}
			_, err = store.CompleteKnowledge(t.Context(), "alice", 94102, 0, expected)
			require.NoError(t, err)
			require.NoError(t, store.ReconcileMemory(t.Context(), "alice", captured))
			stored, err := store.Knowledge(t.Context(), "alice", 94102)
			require.NoError(t, err)
			require.Len(t, stored, 1)
			assert.Equal(
				t,
				expected,
				stored[0],
				"an older captured epoch must not erase a newer read or regress its epoch",
			)
		})
	}
}
