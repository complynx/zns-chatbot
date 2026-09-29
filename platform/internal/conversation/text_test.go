package conversation_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func historyDatabase(t *testing.T) conversation.Service {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "history_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	return conversation.Service{DB: db}
}

type historyBarrierKey struct{}

type historyQueryBarrier struct {
	query   string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *historyQueryBarrier) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	return context.WithValue(ctx, historyBarrierKey{}, strings.Contains(data.SQL, b.query))
}

func (b *historyQueryBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if selected, _ := ctx.Value(historyBarrierKey{}).(bool); selected {
		b.once.Do(func() { close(b.entered); <-b.release })
	}
}

func barrierService(t *testing.T, s conversation.Service, query string) (conversation.Service, *historyQueryBarrier) {
	t.Helper()
	barrier := &historyQueryBarrier{query: query, entered: make(chan struct{}), release: make(chan struct{})}
	cfg := s.DB.Config()
	cfg.ConnConfig.Tracer = barrier
	db, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return conversation.Service{DB: db}, barrier
}

func TestHistoryDeletionConcurrentBarriers(t *testing.T) {
	t.Parallel()
	s := historyDatabase(t)
	id := longEvent(t, s, strings.Repeat("private canary 🌍", 1000))
	reader, readBarrier := barrierService(t, s, "-- name: ReadText")
	type readResult struct {
		chunk conversation.TextChunk
		err   error
	}
	result := make(chan readResult, 1)
	go func() {
		chunk, err := reader.ReadText(t.Context(), "alice", id, 0, 4000, "")
		result <- readResult{chunk: chunk, err: err}
	}()
	<-readBarrier.entered
	require.NoError(t, s.DeleteContent(t.Context(), "alice", id))
	close(readBarrier.release)
	read := <-result
	require.NoError(t, read.err)
	generation, err := s.Generation(t.Context(), "alice")
	require.NoError(t, err)
	// A read already in progress is stamped with its old generation so the host
	// must discard it even when the completion arrives after deletion commits.
	assert.Less(t, read.chunk.Generation, generation)
	assert.Contains(t, read.chunk.Text, "private canary")
	require.NoError(t, s.Append(t.Context(), "alice", "short-after-delete", "assistant", "short body"))
	page, err := s.Read(t.Context(), "alice", conversation.Query{Limit: 1})
	require.NoError(t, err)
	id = page.Events[0].ID
	summarizer, summaryBarrier := barrierService(t, s, "-- name: KnownActor")
	committed := make(chan error, 1)
	go func() {
		committed <- summarizer.CommitSummary(t.Context(), "alice", 1, []int64{id}, "short body summarized")
	}()
	<-summaryBarrier.entered
	require.NoError(t, s.DeleteContent(t.Context(), "alice", id))
	close(summaryBarrier.release)
	require.ErrorContains(t, <-committed, "conversation summary changed")
	window, err := s.Window(t.Context(), "alice", 2)
	require.NoError(t, err)
	assert.Empty(t, window.Summary.Text)
	assert.EqualValues(t, 2, window.Generation)
}

func longEvent(t *testing.T, s conversation.Service, body string) int64 {
	t.Helper()
	var id int64
	err := s.DB.QueryRow(t.Context(), `INSERT INTO core.conversation_events(owner,source_key,kind,text,created_at)
 VALUES('alice',$1,'user','[Full text available through history.read]','2035-01-01Z') RETURNING id`, rand.Text()).Scan(&id)
	require.NoError(t, err)
	_, err = s.DB.Exec(
		t.Context(),
		`INSERT INTO core.conversation_message_bodies(event_id,body,body_sha256,character_count)
 VALUES($1,$2,$3,char_length($2))`,
		id,
		body,
		fmt.Sprintf("%x", sha256.Sum256([]byte(body))),
	)
	require.NoError(t, err)
	return id
}

func TestHistoryFullTextPrivacyAndSummary(t *testing.T) {
	t.Parallel()
	s := historyDatabase(t)
	controls := string(
		[]rune{
			1,
			2,
			3,
			4,
			5,
			6,
			7,
			8,
			9,
			10,
			11,
			12,
			13,
			14,
			15,
			16,
			17,
			18,
			19,
			20,
			21,
			22,
			23,
			24,
			25,
			26,
			27,
			28,
			29,
			30,
			31,
		},
	)
	body := strings.Repeat("Юникод🌍"+controls+"<&>\"\\", 5000) + "LAST-CHARACTER-Я"
	id := longEvent(t, s, body)
	require.NoError(t, s.Append(t.Context(), "alice", "short", "assistant", "ordinary answer"))
	page, err := s.Read(t.Context(), "alice", conversation.Query{Limit: 20})
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	assert.True(t, page.Events[1].HasFullText)
	assert.Equal(t, 2035, page.Events[1].At.Year())
	small, err := s.ReadText(t.Context(), "alice", page.Events[0].ID, 0, 4000, "")
	require.NoError(t, err)
	assert.Equal(t, "ordinary answer", small.Text)
	assert.False(t, small.More)
	_, err = s.DB.Exec(
		t.Context(),
		`UPDATE core.conversation_message_bodies SET body=$2 WHERE event_id=$1`,
		id,
		"changed body",
	)
	require.Error(t, err, "the complete body and its digest must agree")
	_, err = s.DB.Exec(
		t.Context(),
		`UPDATE core.conversation_message_bodies SET body=$2 WHERE event_id=$1`,
		id,
		"NUL\x00body",
	)
	require.Error(t, err, "PostgreSQL-incompatible source bytes must fail, never disappear")
	_, err = s.ReadText(t.Context(), "bob", id, 0, 4000, "")
	require.ErrorContains(t, err, "history_missing")
	_, missing := s.ReadText(t.Context(), "bob", id+1000, 0, 4000, "")
	assert.Equal(t, err.Error(), missing.Error())
	var rebuilt strings.Builder
	offset := 0
	digest := ""
	chunks := 0
	for {
		// Recreate the service each time: navigation has no process-local state.
		restarted := conversation.Service{DB: s.DB}
		chunk, readErr := restarted.ReadText(t.Context(), "alice", id, offset, 4000, digest)
		require.NoError(t, readErr)
		rebuilt.WriteString(chunk.Text)
		digest = chunk.Digest
		chunks++
		if !chunk.More {
			break
		}
		require.Greater(t, chunk.NextOffset, offset)
		offset = chunk.NextOffset
	}
	assert.Greater(t, chunks, 8)
	assert.Equal(t, body, rebuilt.String())
	_, err = s.ReadText(t.Context(), "alice", id, 0, 4000, strings.Repeat("a", 64))
	require.ErrorContains(t, err, "history_stale")
	batch, err := s.SummaryBatch(t.Context(), "alice", page.Events[0].ID+1)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, "ordinary answer", batch[0].Text)
	require.Error(t, s.CommitSummary(t.Context(), "alice", 0, []int64{id}, "excerpt only"))
	require.NoError(t, s.CommitSummary(t.Context(), "alice", 0, []int64{batch[0].ID}, "ordinary summary"))
	_, err = s.DB.Exec(
		t.Context(),
		`INSERT INTO core.legacy_message_references(source_key,event_id,owner,bot_namespace,collection,identity_sha256,source_record_sha256,resolved_at,plan_sha256,resolution_sha256,disposition)
 VALUES(repeat('a',64),$1,'alice','synthetic','messages',repeat('b',64),repeat('c',64),'2035-01-01Z',repeat('d',64),repeat('e',64),'retained')`,
		id,
	)
	require.NoError(t, err)
	require.ErrorContains(t, s.DeleteContent(t.Context(), "bob", id), "history_missing")
	require.NoError(t, s.DeleteContent(t.Context(), "alice", id))
	require.Error(t, s.CommitSummary(t.Context(), "alice", 1, []int64{batch[0].ID}, "old in-flight summary"))
	_, err = s.ReadText(t.Context(), "alice", id, 0, 4000, digest)
	require.ErrorContains(t, err, "history_stale")
	omitted, err := s.ReadText(t.Context(), "alice", id, 0, 4000, "")
	require.NoError(t, err)
	assert.True(t, omitted.Omitted)
	assert.Empty(t, omitted.Text)
	assert.EqualValues(t, 1, omitted.Generation)
	require.NoError(t, s.DeleteContent(t.Context(), "alice", id))
	window, err := s.Window(t.Context(), "alice", 2)
	require.NoError(t, err)
	assert.Empty(t, window.Summary.Text)
	assert.EqualValues(t, 2, window.Summary.Version)
	assert.EqualValues(t, 1, window.Generation)
	require.Len(t, window.Recent, 2)
	assert.Equal(t, "deleted", window.Recent[0].OmissionReason)
	var tombstone bool
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT tombstoned FROM core.legacy_message_references WHERE event_id=$1`, id).
			Scan(&tombstone),
	)
	assert.True(t, tombstone)
}
